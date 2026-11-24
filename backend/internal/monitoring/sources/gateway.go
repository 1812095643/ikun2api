package sources

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Batch struct {
	Observations []domain.Observation
	Cursor       domain.Cursor
	Capabilities []domain.Capability
}
type EvidenceSource interface {
	Name() string
	Poll(context.Context, domain.Cursor, time.Time) (Batch, error)
}

var ErrIncompatible = errors.New("incompatible evidence schema")
var ErrSourceUnavailable = errors.New("evidence source unavailable")

type Gateway struct {
	URL, Token string
	Client     *http.Client
}

func (g *Gateway) Name() string { return "gateway" }
func (g *Gateway) Poll(ctx context.Context, c domain.Cursor, now time.Time) (Batch, error) {
	u, err := url.Parse(g.URL)
	if err != nil || u.User != nil || u.RawQuery != "" || !net.ParseIP(u.Hostname()).IsLoopback() {
		return Batch{}, errors.New("gateway evidence must use a literal loopback address")
	}
	q := u.Query()
	q.Set("after", c.Watermark)
	q.Set("boot_id", c.BootID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	client := g.Client
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
	}
	resp, err := client.Do(req)
	if err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Batch{}, ErrSourceUnavailable
	}
	var snap domain.Snapshot
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&snap); err != nil {
		return Batch{}, ErrIncompatible
	}
	if snap.Schema != domain.SchemaVersion || snap.Site == "" || snap.BootID == "" {
		return Batch{}, ErrIncompatible
	}
	gap := c.BootID != "" && c.BootID != snap.BootID
	seq, _ := strconv.ParseUint(c.Watermark, 10, 64)
	gap = gap || (c.BootID == snap.BootID && seq > 0 && snap.Oldest > seq+1) || snap.Dropped > c.Dropped
	obs := append(snap.Events, snap.Active...)
	clockSkew := false
	for i := range obs {
		obs[i].ObservedAt = now
		if obs[i].OccurredAt.After(now.Add(5 * time.Second)) {
			clockSkew = true
			obs[i].Quality = domain.Stale
		}
		if err = obs[i].Validate(); err != nil {
			return Batch{}, ErrIncompatible
		}
	}
	if clockSkew {
		gap = true
		obs = append(obs, domain.Observation{Schema: 1, Site: snap.Site, Source: "gateway", EventID: "clock:" + strconv.FormatInt(now.Unix()/30, 10), Kind: "source.gap", TaskID: "gateway", OccurredAt: now, ObservedAt: now, Quality: domain.Partial, Reason: "source_clock_ahead"})
	}
	if gap {
		obs = append(obs, domain.Observation{Schema: 1, Site: snap.Site, Source: "gateway", EventID: "gap:" + snap.BootID + ":" + strconv.FormatUint(snap.Sequence, 10), Kind: "source.gap", OccurredAt: now, ObservedAt: now, Quality: domain.Partial, Reason: "cache_or_restart_gap", Metrics: map[string]float64{"dropped": float64(snap.Dropped)}})
	}
	quality := domain.Fresh
	if gap {
		quality = domain.Partial
	}
	return Batch{Observations: obs, Capabilities: snap.Capabilities, Cursor: domain.Cursor{Source: g.Name(), Watermark: strconv.FormatUint(snap.Sequence, 10), BootID: snap.BootID, LastSuccess: now, Quality: quality, Dropped: snap.Dropped}}, nil
}
func Authorized(header, token string) bool {
	return len(token) >= 32 && subtle.ConstantTimeCompare([]byte(header), []byte("Bearer "+token)) == 1
}
