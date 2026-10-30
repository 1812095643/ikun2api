package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
)

func digestString(s string) string { d := sha256.Sum256([]byte(s)); return hex.EncodeToString(d[:]) }

func upstreamBalanceGroups(accounts []service.Account) []upstreamBalanceGroup {
	groups := map[string]*upstreamBalanceGroup{}
	for _, a := range accounts {
		if a.Type != "apikey" {
			continue
		}
		urls := []string{}
		if base := strings.TrimSpace(a.GetCredential("base_url")); base != "" {
			urls = append(urls, base)
		}
		if bases, ok := a.Credentials["api_base_urls"].(map[string]any); ok {
			for _, v := range bases {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					urls = append(urls, s)
				}
			}
		}
		if len(urls) == 0 {
			base := a.GetOpenAIBaseURL()
			if base == "" {
				switch a.Platform {
				case "gemini":
					base = "https://generativelanguage.googleapis.com"
				case "grok":
					base = a.GetGrokBaseURL()
				default:
					base = a.GetBaseURL()
				}
			}
			if base != "" {
				urls = append(urls, base)
			}
		}
		seen := map[string]bool{}
		for _, raw := range urls {
			base, err := normalizeUpstreamBalanceURL(raw)
			if err != nil || seen[base] {
				continue
			}
			seen[base] = true
			group := groups[base]
			if group == nil {
				u, _ := url.Parse(base)
				group = &upstreamBalanceGroup{ID: upstreamBalanceID(base), URL: base, Name: u.Host, Accounts: []upstreamBalanceAccount{}}
				groups[base] = group
			}
			group.Accounts = append(group.Accounts, upstreamBalanceAccount{ID: a.ID, Name: a.Name, Platform: a.Platform, key: strings.TrimSpace(a.GetCredential("api_key")), proxyID: a.ProxyID})
		}
	}
	result := make([]upstreamBalanceGroup, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.Accounts, func(i, j int) bool { return g.Accounts[i].ID < g.Accounts[j].ID })
		var signature strings.Builder
		for _, a := range g.Accounts {
			proxyID := ""
			if a.proxyID != nil {
				proxyID = strconv.FormatInt(*a.proxyID, 10)
			}
			signature.WriteString(strconv.FormatInt(a.ID, 10) + ":" + digestString(a.key) + ":" + proxyID + "\n")
		}
		g.SourceHash = digestString(signature.String())
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].URL < result[j].URL })
	return result
}
func (h *Handler) discoverUpstreamBalances(ctx context.Context) ([]upstreamBalanceGroup, error) {
	// No group, provider, scheduling or status filter. Disabled API keys are still
	// visible; OAuth/setup-token/cookie accounts never enter this query.
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,platform,type,credentials,proxy_id FROM accounts WHERE deleted_at IS NULL AND type='apikey' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []service.Account{}
	for rows.Next() {
		var a service.Account
		var raw []byte
		if err = rows.Scan(&a.ID, &a.Name, &a.Platform, &a.Type, &raw, &a.ProxyID); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &a.Credentials); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return upstreamBalanceGroups(accounts), nil
}
func (h *Handler) syncUpstreamBalances(ctx context.Context, groups []upstreamBalanceGroup) error {
	config, _ := json.Marshal(defaultUpstreamBalanceConfig())
	for _, g := range groups {
		_, err := h.db.ExecContext(ctx, `INSERT INTO workspace_upstream_balances(id,url,name,config,source_hash) VALUES($1,$2,$3,$4,$5)
   ON CONFLICT(id) DO UPDATE SET source_hash=EXCLUDED.source_hash,revision=workspace_upstream_balances.revision+1,snapshot='{}',alert='{}',last_checked_at=NULL,next_check_at=NOW(),updated_at=NOW()
   WHERE workspace_upstream_balances.source_hash<>EXCLUDED.source_hash`, g.ID, g.URL, g.Name, string(config), g.SourceHash)
		if err != nil {
			return err
		}
	}
	return nil
}

type upstreamBalanceRecord struct {
	ID              string                   `json:"id"`
	URL             string                   `json:"url"`
	Name            string                   `json:"name"`
	Config          upstreamBalanceConfig    `json:"config"`
	TokenConfigured bool                     `json:"token_configured"`
	Accounts        []upstreamBalanceAccount `json:"accounts"`
	KeyCount        int                      `json:"key_count"`
	Snapshot        upstreamBalanceSnapshot  `json:"snapshot"`
	Alert           upstreamBalanceAlert     `json:"alert"`
	Queued          bool                     `json:"queued"`
	Checking        bool                     `json:"checking"`
	LastCheckedAt   *time.Time               `json:"last_checked_at"`
	NextCheckAt     *time.Time               `json:"next_check_at"`
	secret          string
	revision        int64
	sourceHash      string
	leaseUntil      *time.Time
	storedAlert     upstreamBalanceAlertStored
}

func (h *Handler) loadUpstreamBalanceRecords(ctx context.Context, groups []upstreamBalanceGroup) ([]upstreamBalanceRecord, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id,url,name,config,secret_blob,source_hash,revision,snapshot,alert,refresh_requested,lease_until,last_checked_at,next_check_at FROM workspace_upstream_balances ORDER BY url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	current := map[string]upstreamBalanceGroup{}
	for _, g := range groups {
		current[g.ID] = g
	}
	records := []upstreamBalanceRecord{}
	for rows.Next() {
		var r upstreamBalanceRecord
		var c, s, a []byte
		if err = rows.Scan(&r.ID, &r.URL, &r.Name, &c, &r.secret, &r.sourceHash, &r.revision, &s, &a, &r.Queued, &r.leaseUntil, &r.LastCheckedAt, &r.NextCheckAt); err != nil {
			return nil, err
		}
		g, ok := current[r.ID]
		if !ok {
			continue
		}
		r.Config = defaultUpstreamBalanceConfig()
		if err = json.Unmarshal(c, &r.Config); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(s, &r.Snapshot); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(a, &r.storedAlert); err != nil {
			return nil, err
		}
		r.Alert = r.storedAlert.upstreamBalanceAlert
		r.TokenConfigured = r.secret != ""
		r.Accounts = g.Accounts
		r.Checking = r.leaseUntil != nil && r.leaseUntil.After(time.Now())
		if r.Config.Recipients == nil {
			r.Config.Recipients = []string{}
		}
		if r.Snapshot.Readings == nil {
			r.Snapshot.Readings = []upstreamBalanceReading{}
		}
		keys := map[string]bool{}
		for _, a := range g.Accounts {
			if a.key != "" {
				keys[a.key] = true
			}
		}
		r.KeyCount = len(keys)
		records = append(records, r)
	}
	return records, rows.Err()
}
func (h *Handler) prepareUpstreamBalances(ctx context.Context) ([]upstreamBalanceGroup, []upstreamBalanceRecord, error) {
	groups, err := h.discoverUpstreamBalances(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err = h.syncUpstreamBalances(ctx, groups); err != nil {
		return nil, nil, err
	}
	records, err := h.loadUpstreamBalanceRecords(ctx, groups)
	return groups, records, err
}
