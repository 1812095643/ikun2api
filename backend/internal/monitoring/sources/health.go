package sources

import (
	"bufio"
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/lib/pq"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type LocalHealth struct {
	Site, DiskPath, GatewayURL, RedisAddress, RedisPassword, CertificateFile string
	DB                                                                       *sql.DB
	CPUSamples                                                               []float64
	CPUStart                                                                 time.Time
	lastOOM                                                                  int64
}

func (h *LocalHealth) Name() string { return "local.health" }
func (h *LocalHealth) Poll(ctx context.Context, c domain.Cursor, now time.Time) (Batch, error) {
	out := []domain.Observation{}
	add := func(kind, target, reason string, m map[string]float64) {
		out = append(out, domain.Observation{Schema: 1, Site: h.Site, Source: h.Name(), EventID: target + ":" + strconv.FormatInt(now.Unix()/15, 10), Kind: kind, TaskID: target, Reason: reason, OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, Metrics: m})
	}
	metrics := map[string]float64{}
	path := h.DiskPath
	if path == "" {
		path = "/"
	}
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		metrics["memory_used_percent"] = m.UsedPercent
	}
	if d, err := disk.UsageWithContext(ctx, path); err == nil {
		metrics["disk_free_percent"] = 100 - d.UsedPercent
		if d.InodesTotal > 0 {
			metrics["inode_free_percent"] = 100 * float64(d.InodesFree) / float64(d.InodesTotal)
		}
	}
	if values, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(values) > 0 {
		if h.CPUStart.IsZero() {
			h.CPUStart = now
		}
		h.CPUSamples = append(h.CPUSamples, values[0])
		if len(h.CPUSamples) > 9 {
			h.CPUSamples = h.CPUSamples[len(h.CPUSamples)-9:]
		}
		if len(h.CPUSamples) == 9 && now.Sub(h.CPUStart) >= 2*time.Minute {
			lowest := 100.
			for _, v := range h.CPUSamples {
				if v < lowest {
					lowest = v
				}
			}
			metrics["cpu_sustained_percent"] = lowest
		}
	}
	if b, err := os.ReadFile("/proc/vmstat"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "oom_kill" {
				n, e := strconv.ParseInt(f[1], 10, 64)
				if e == nil {
					if h.lastOOM > 0 && n >= h.lastOOM {
						metrics["oom_delta"] = float64(n - h.lastOOM)
					}
					h.lastOOM = n
				}
			}
		}
	}
	if h.CertificateFile != "" {
		b, err := os.ReadFile(h.CertificateFile)
		if err == nil {
			block, _ := pem.Decode(b)
			if block != nil {
				cert, e := x509.ParseCertificate(block.Bytes)
				if e == nil {
					metrics["cert_remaining_seconds"] = cert.NotAfter.Sub(now).Seconds()
				}
			}
		}
		if _, ok := metrics["cert_remaining_seconds"]; !ok {
			add("source.unavailable", "certificate", "certificate_unreadable", nil)
		}
	}
	if len(metrics) > 0 {
		add("host.resources", "host", "sampled", metrics)
	} else {
		add("source.unavailable", "host", "metrics_unavailable", nil)
	}
	if h.GatewayURL != "" {
		healthy := false
		reason := "unreachable"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.GatewayURL, nil)
		if err == nil && net.ParseIP(req.URL.Hostname()).IsLoopback() && req.URL.User == nil {
			client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, e := client.Do(req)
			if e == nil {
				healthy = resp.StatusCode == 200
				resp.Body.Close()
				client.CloseIdleConnections()
			}
			if healthy {
				reason = "reachable"
			}
		}
		v := 0.
		if healthy {
			v = 1
		}
		add("dependency.health", "gateway", reason, map[string]float64{"healthy": v})
	}
	if h.DB != nil {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := h.DB.PingContext(probe)
		cancel()
		value := 0.
		if err == nil {
			value = 1
		}
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && (pgErr.Code == "28P01" || pgErr.Code == "28000") {
			add("source.unavailable", "postgres", "monitor_authentication_missing", nil)
		} else {
			add("dependency.health", "postgres", "ping_result", map[string]float64{"healthy": value})
		}
	}
	if h.RedisAddress != "" {
		host, _, err := net.SplitHostPort(h.RedisAddress)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			add("source.unavailable", "redis", "nonlocal_address_rejected", nil)
		} else {
			healthy, authNeeded := redisHealth(ctx, h.RedisAddress, h.RedisPassword)
			if authNeeded {
				add("source.unavailable", "redis", "monitor_authentication_missing", nil)
			} else {
				v := 0.
				if healthy {
					v = 1
				}
				add("dependency.health", "redis", "ping_result", map[string]float64{"healthy": v})
			}
		}
	}
	return Batch{Observations: out, Cursor: domain.Cursor{Source: h.Name(), LastSuccess: now, Quality: domain.Fresh}}, nil
}
func redisHealth(ctx context.Context, address, password string) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return false, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReaderSize(conn, 1024)
	if password != "" {
		if _, err = fmt.Fprintf(conn, "*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n", len(password), password); err != nil {
			return false, false
		}
		line, e := reader.ReadString('\n')
		if e != nil || !strings.HasPrefix(line, "+OK") {
			return false, true
		}
	}
	if _, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		return false, false
	}
	line, err := reader.ReadString('\n')
	if strings.HasPrefix(line, "-NOAUTH") {
		return false, true
	}
	return err == nil && line == "+PONG\r\n", false
}
