// ops-monitor runs independently of the gateway's dependency graph.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/adminapi"
	"github.com/MACOS-DO/sub4api/internal/monitoring/channels"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/ownership"
	monitor "github.com/MACOS-DO/sub4api/internal/monitoring/runtime"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var build = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ops-monitor stopped:", err)
		os.Exit(1)
	}
}
func run() error {
	socket := flag.String("socket", "/run/ikun-ops-monitor/admin.sock", "restricted Unix socket for authenticated gateway proxy")
	state := flag.String("state", "/var/lib/ikun-ops-monitor/state.db", "SQLite state path")
	health := flag.String("health", "127.0.0.1:9188", "loopback health listener")
	gateway := flag.String("gateway", "", "loopback gateway evidence endpoint")
	backup := flag.String("backup", "", "write consistent snapshot then exit")
	flag.Parse()
	s, err := store.Open(*state)
	if err != nil {
		return fmt.Errorf("local state unavailable")
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *backup != "" {
		return s.Backup(ctx, *backup)
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*health)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("health listener must use loopback")
	}
	status := &monitor.Health{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snapshot := status.Snapshot(time.Now())
		if !snapshot.Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(map[string]any{"process": "running", "build": build, "schema": domain.SchemaVersion, "runtime": snapshot})
	})
	srv := &http.Server{Addr: *health, Handler: mux, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", *health)
	if err != nil {
		return fmt.Errorf("health listener unavailable")
	}
	defer listener.Close()
	go srv.Serve(listener)
	activated := struct {
		At time.Time `json:"at"`
	}{At: time.Now().UTC()}
	if err = s.State(ctx, "activation", &activated); err != nil {
		return err
	}
	if err = s.SaveState(ctx, "activation", activated); err != nil {
		return err
	}
	clock := monitor.WallClock{}
	var businessDB *sql.DB
	defer func() {
		if businessDB != nil {
			businessDB.Close()
		}
	}()
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	runCollector := func(c *monitor.Collector, d time.Duration) {
		c.Report = status.Record
		workers.Add(1)
		go func() { defer workers.Done(); c.Run(ctx, d) }()
	}
	healthSource := &sources.LocalHealth{Site: cfg.Site, DiskPath: filepath.Dir(*state), GatewayURL: os.Getenv("OPS_GATEWAY_HEALTH_URL"), RedisAddress: os.Getenv("OPS_REDIS_ADDRESS"), RedisPassword: os.Getenv("OPS_REDIS_PASSWORD"), CertificateFile: os.Getenv("OPS_CERTIFICATE_FILE")}

	if *gateway != "" {
		c := &monitor.Collector{Store: s, Clock: clock, Site: cfg.Site, Source: &sources.Gateway{URL: *gateway, Token: os.Getenv("OPS_OBSERVATION_TOKEN")}}
		runCollector(c, 5*time.Second)
	}
	if dsn := os.Getenv("OPS_POSTGRES_DSN"); dsn != "" {
		db, e := sources.OpenPostgres(dsn)
		if e != nil {
			return fmt.Errorf("invalid evidence database configuration")
		}
		businessDB = db
		healthSource.DB = db
		for _, view := range sources.ViewNames {
			c := &monitor.Collector{Store: s, Clock: clock, Site: cfg.Site, Source: &sources.Postgres{Timezone: os.Getenv("OPS_BUSINESS_TIMEZONE"), DB: db, View: view, Site: cfg.Site, ActivatedAt: activated.At}}
			runCollector(c, 15*time.Second)
			if !sources.IsSnapshot(view) {
				reconcile := &monitor.Collector{Store: s, Clock: clock, Site: cfg.Site, Source: &sources.Postgres{Timezone: os.Getenv("OPS_BUSINESS_TIMEZONE"), DB: db, View: view, Site: cfg.Site, ActivatedAt: activated.At, Lookback: 24 * time.Hour, Reconcile: true}}
				runCollector(reconcile, 15*time.Second)
			}
		}
	}
	runCollector(&monitor.Collector{Store: s, Clock: clock, Site: cfg.Site, Source: healthSource}, 15*time.Second)
	manager := &incidents.Manager{Store: s}
	// Channel credentials set on the admin page are sealed with a key kept next to the state.
	registry, err := channels.Open(s, filepath.Join(filepath.Dir(*state), "channel.key"), channels.FromEnv())
	if err != nil {
		return fmt.Errorf("channel key unavailable")
	}
	if *socket != "" {
		uds, e := adminapi.Listen(*socket)
		if e != nil {
			return fmt.Errorf("admin socket unavailable")
		}
		defer uds.Close()
		manager.OwnershipPath = filepath.Join(filepath.Dir(*socket), "ownership.json")
		if e = ownership.Write(manager.OwnershipPath, ownership.Project(cfg)); e != nil {
			return fmt.Errorf("ownership projection unavailable")
		}
		api := &adminapi.API{Store: s, Manager: manager, Build: build, Channels: registry, Names: notify.Names{DB: businessDB}}
		// Test sends and credential checks call Feishu/SMTP synchronously (up to ~15s).
		local := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 30 * time.Second}
		go local.Serve(uds)
		defer func() {
			stop, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			local.Shutdown(stop)
		}()
	}
	evaluator := &monitor.Evaluator{Report: status.Record, Store: s, Manager: manager, Clock: clock}
	sender := &notify.Worker{Report: status.Record, Store: s, Senders: map[string]notify.Sender{
		"feishu": notify.ReadableSender{DB: businessDB, Next: channels.Sender{Registry: registry, Channel: "feishu"}},
		"email":  notify.ReadableSender{DB: businessDB, Next: channels.Sender{Registry: registry, Channel: "email"}},
	}}
	for _, run := range []func(context.Context){evaluator.Run, sender.Run} {
		workers.Add(1)
		go func(run func(context.Context)) { defer workers.Done(); run(ctx) }(run)
	}
	fmt.Println("ops-monitor running; state directory:", filepath.Dir(*state), "; initial shadow:", cfg.Shadow)
	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	return srv.Shutdown(shutdown)
}
