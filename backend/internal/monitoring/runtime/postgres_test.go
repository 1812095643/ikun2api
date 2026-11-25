package runtime_test

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/MACOS-DO/sub4api/ent"
	_ "github.com/MACOS-DO/sub4api/ent/runtime"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/repository"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 一个隔离库覆盖迁移、事件事务、租约恢复、目标变更和保留期，不运行生产探测。
func TestEmbeddedMonitorPostgres(t *testing.T) {
	dsn := os.Getenv("OPS_MONITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立的本地 OPS_MONITOR_TEST_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(16)
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	for _, name := range []string{"282_ops_monitor_embedded.sql", "283_ops_monitor_evidence_views.sql"} {
		raw, e := migrations.FS.ReadFile(name)
		require.NoError(t, e)
		_, e = db.ExecContext(ctx, string(raw))
		require.NoError(t, e)
	}
	for _, view := range sources.ViewNames {
		source := sources.Postgres{DB: db, View: view, Site: "test", ActivatedAt: time.Now().Add(-time.Minute)}
		_, e := source.Poll(ctx, domain.Cursor{Source: source.Name()}, time.Now().UTC())
		require.NoError(t, e, view)
	}
	// 使用同一个隔离库验证路由改写复用调用方事务及旧 JSON null 数据。
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	for _, contextTx := range []bool{false, true} {
		tx, e := client.Tx(ctx)
		require.NoError(t, e)
		txClient := tx.Client()
		u, e := txClient.User.Create().SetEmail(uuid.NewString() + "@example.test").SetPasswordHash("test-only").Save(ctx)
		require.NoError(t, e)
		g1, e := txClient.Group.Create().SetName("first-" + uuid.NewString()).SetPlatform("openai").Save(ctx)
		require.NoError(t, e)
		g2, e := txClient.Group.Create().SetName("second-" + uuid.NewString()).SetPlatform("openai").Save(ctx)
		require.NoError(t, e)
		key, e := txClient.APIKey.Create().SetName("route").SetUserID(u.ID).SetKey(uuid.NewString()).SetGroupID(g1.ID).SetRouteGroupIds([]int64{g1.ID, g2.ID}).Save(ctx)
		require.NoError(t, e)
		_, e = txClient.APIKey.Create().SetName("old-null").SetUserID(u.ID).SetKey(uuid.NewString()).SetGroupID(g1.ID).SetRouteGroupIds(nil).Save(ctx)
		require.NoError(t, e)
		rewriteCtx := ctx
		if contextTx {
			rewriteCtx = dbent.NewTxContext(ctx, tx)
		}
		count, e := repository.NewAPIKeyRepository(txClient, db).(interface {
			RemoveGroupFromSmartRoutes(context.Context, int64) (int64, error)
		}).RemoveGroupFromSmartRoutes(rewriteCtx, g1.ID)
		require.NoError(t, e)
		require.EqualValues(t, 1, count)
		var outside int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM api_keys WHERE id=$1", key.ID).Scan(&outside))
		require.Zero(t, outside)
		require.NoError(t, tx.Rollback())
	}
	s := store.New(db)
	manager := incidents.Manager{Store: s}
	cipher, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("01", 32)}})
	require.NoError(t, err)
	cid, tid := uuid.NewString(), uuid.NewString()
	input := domain.ChannelInput{Channel: domain.Channel{ID: cid, Kind: "telegram", Name: "测试通道", Enabled: true, Settings: map[string]string{}}, Secrets: map[string]string{"bot_token": "123456:test-secret"}, Targets: []domain.Target{{ID: tid, Name: "测试对象", Address: "-100123", Enabled: true}}}
	require.NoError(t, s.SaveChannel(ctx, input, "test-admin", cipher))
	channels, err := s.Channels(ctx)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Contains(t, channels[0].SecretConfigured, "bot_token")
	var encrypted string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT secret_ciphertext FROM ops_monitor_channels WHERE id=$1", cid).Scan(&encrypted))
	require.NotContains(t, encrypted, "test-secret")
	input.Revision = 1
	input.Secrets = map[string]string{"bot_token": ""}
	require.NoError(t, s.SaveChannel(ctx, input, "test-admin", cipher))
	_, secret, err := s.ChannelSecret(ctx, cid, cipher)
	require.NoError(t, err)
	require.Equal(t, "123456:test-secret", secret["bot_token"])
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "test-admin"
	cfg.RequestID = uuid.NewString()
	cfg.Rules["S01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.Policies = []domain.NotificationPolicy{{ID: "test", Name: "测试规则", Enabled: true, TargetIDs: []string{tid}, Severities: []string{"P1"}}}
	require.NoError(t, manager.Configure(ctx, cfg, 0))
	cfg, err = s.Config(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	finding := domain.Finding{Rule: "S01", RuleVersion: cfg.Rule("S01").Version, Fingerprint: uuid.NewString(), Severity: "P1", At: now, EvidenceID: "event-1", Scope: "group:5/model:test", Platform: "openai", GroupID: 5, AccountID: 8, RequestID: "r1", UserID: 9, Quality: domain.Fresh, Explanation: domain.Explanation{Conclusion: "候选账号为空"}}
	require.NoError(t, manager.Apply(ctx, finding, cfg, now))
	require.NoError(t, manager.Apply(ctx, finding, cfg, now))
	page, err := s.ListIncidents(ctx, store.IncidentFilter{Status: "active", GroupID: 5, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	id := page.Items[0].ID
	require.Equal(t, 1, page.Items[0].RequestCount)
	deliveries, err := s.Deliveries(ctx)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	var leased atomic.Int32
	var wg sync.WaitGroup
	for index := 0; index < 4; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, e := s.Lease(ctx, now.Add(time.Second), time.Second)
			if e != nil {
				t.Error(e)
				return
			}
			if d != nil {
				if e = s.MarkDeliverySending(ctx, *d, now.Add(time.Second)); e != nil {
					t.Error(e)
				}
				leased.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, leased.Load())
	// 新 Store 模拟服务重启，过期任务可重新领取且明确标记回执不确定。
	restarted := store.New(db)
	d, err := restarted.Lease(ctx, now.Add(3*time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, d)
	require.True(t, d.RecoveredLease)
	d.State = "uncertain"
	d.ErrorClass = "uncertain"
	require.NoError(t, restarted.CompleteDelivery(ctx, *d))
	h, err := s.QueueHealth(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, h.Uncertain)
	i, err := s.Incident(ctx, id)
	require.NoError(t, err)
	require.NoError(t, manager.Control(ctx, id, "ack", "test-admin", uuid.NewString(), i.Revision, time.Time{}, "", now.Add(4*time.Second)))
	i, err = s.Incident(ctx, id)
	require.NoError(t, err)
	require.True(t, i.Acknowledged)
	require.Equal(t, "firing", i.Status)
	require.NoError(t, manager.Control(ctx, id, "silence", "test-admin", uuid.NewString(), i.Revision, now.Add(time.Hour), "维护", now.Add(5*time.Second)))
	i, err = s.Incident(ctx, id)
	require.NoError(t, err)
	require.NoError(t, manager.Control(ctx, id, "close", "test-admin", uuid.NewString(), i.Revision, time.Time{}, "人工关闭", now.Add(6*time.Second)))
	i, err = s.Incident(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "closed", i.Status)
	require.Equal(t, "manual", i.ClosedBy)
	finding.At = now.Add(7 * time.Second)
	finding.EvidenceID = "event-2"
	finding.RequestID = "r2"
	require.NoError(t, manager.Apply(ctx, finding, cfg, finding.At))
	page, err = s.ListIncidents(ctx, store.IncidentFilter{Status: "active", GroupID: 5, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotEqual(t, id, page.Items[0].ID)
	more, err := s.ListIncidents(ctx, store.IncidentFilter{Limit: 1})
	require.NoError(t, err)
	require.True(t, more.HasMore)
	next, err := s.ListIncidents(ctx, store.IncidentFilter{BeforeTime: more.BeforeTime, BeforeID: more.BeforeID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.NotEqual(t, more.Items[0].ID, next.Items[0].ID)
	input.Revision = 2
	input.Targets = nil
	require.NoError(t, s.SaveChannel(ctx, input, "test-admin", cipher))
	_, err = s.Target(ctx, tid)
	require.Error(t, err)
	require.NoError(t, s.Retain(ctx, now.Add(100*24*time.Hour)))
	i, err = s.Incident(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, i, "保留回执待确认的事件")
	// 两位收件人独立处理，成功不会掩盖权限拒绝；失败只转发一次备用对象。
	_, err = db.ExecContext(ctx, `UPDATE ops_monitor_notification_outbox SET state='suppressed' WHERE state='pending'`)
	require.NoError(t, err)
	botTarget, mailTarget := uuid.NewString(), uuid.NewString()
	input.Revision = 3
	input.Targets = []domain.Target{{ID: botTarget, Name: "测试群", Address: "-100123", Enabled: true}}
	require.NoError(t, s.SaveChannel(ctx, input, "test-admin", cipher))
	mailChannel := uuid.NewString()
	require.NoError(t, s.SaveChannel(ctx, domain.ChannelInput{Channel: domain.Channel{ID: mailChannel, Kind: "email", Name: "备用邮箱", Enabled: true, Settings: map[string]string{}}, Targets: []domain.Target{{ID: mailTarget, Name: "备用收件人", Address: "review@example.test", Enabled: true}}}, "test-admin", cipher))
	cfg.Policies = []domain.NotificationPolicy{{ID: "delivery", Name: "通知验证", Enabled: true, TargetIDs: []string{botTarget}, FallbackTargetIDs: []string{mailTarget}}}
	cfg.Actor = "test-admin"
	cfg.RequestID = uuid.NewString()
	require.NoError(t, manager.Configure(ctx, cfg, cfg.Version))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":false,"error_code":403,"description":"bot was blocked by the user"}`)
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	var mails atomic.Int32
	worker := notify.Worker{Store: s, Cipher: cipher, Sender: &notify.Sender{Client: &http.Client{Transport: localNotificationTransport{serverURL}}, Email: func(context.Context, string, notify.Message) error { mails.Add(1); return nil }}}
	incident := domain.Incident{ID: uuid.NewString(), Fingerprint: uuid.NewString(), Rule: "S01", RuleVersion: cfg.Rule("S01").Version, Revision: 1, Status: "firing", Severity: "P1", FirstSeen: now, LastSeen: now, Quality: domain.Fresh}
	job := domain.Delivery{ID: uuid.NewString(), IncidentID: incident.ID, Revision: 1, Channel: cid, Target: botTarget, Kind: "firing", State: "pending", NextAttempt: now, FallbackTargetIDs: []string{mailTarget}}
	require.NoError(t, s.SaveIncident(ctx, incident, []domain.Delivery{job}))
	require.NoError(t, worker.Tick(ctx, now.Add(time.Minute)))
	require.NoError(t, worker.Tick(ctx, now.Add(time.Minute)))
	log, err := s.DeliveryPage(ctx, incident.ID, "", "", 10)
	require.NoError(t, err)
	require.Len(t, log, 2)
	states := map[string]string{}
	for _, d := range log {
		states[d.Target] = d.State
	}
	require.Equal(t, "dead", states[botTarget])
	require.Equal(t, "accepted", states[mailTarget])
	require.EqualValues(t, 1, mails.Load())
	channels, err = s.Channels(ctx)
	require.NoError(t, err)
	require.Len(t, channels, 2)
	for _, channel := range channels {
		if channel.ID == mailChannel {
			require.Equal(t, "accepted", channel.LastState)
			require.NotNil(t, channel.LastAccepted)
		}
	}
	// 已有资源和依赖采样可证明恢复；仅有日志错误的来源仍保留不完整标记。
	_, err = db.ExecContext(ctx, `INSERT INTO ops_system_metrics(created_at,memory_usage_percent,db_ok,redis_ok) VALUES(NOW(),30,true,true)`)
	require.NoError(t, err)
	for _, view := range []string{"metrics", "dependencies"} {
		source := sources.Postgres{DB: db, View: view, Site: "test", ActivatedAt: now.Add(-time.Minute)}
		batch, e := source.Poll(ctx, domain.Cursor{}, time.Now().Add(time.Second))
		require.NoError(t, e)
		require.NotEmpty(t, batch.Observations)
		for _, observation := range batch.Observations {
			require.Equal(t, domain.Fresh, observation.Quality)
		}
	}
	cfg, err = s.Config(ctx)
	require.NoError(t, err)
	parent := domain.Finding{Rule: "S03", RuleVersion: cfg.Rule("S03").Version, Fingerprint: uuid.NewString(), Severity: "P1", At: now, EvidenceID: "proxy-fault", Scope: "proxy:1", Quality: domain.Fresh}
	require.NoError(t, manager.Apply(ctx, parent, cfg, now))
	child := parent
	child.ParentFingerprint = parent.Fingerprint
	child.Fingerprint = uuid.NewString()
	child.Scope = "account:1"
	child.EvidenceID = "account-fault"
	require.NoError(t, manager.Apply(ctx, child, cfg, now))
	child.Healthy = true
	child.At = now.Add(time.Second)
	child.EvidenceID = "account-healthy"
	require.NoError(t, manager.Apply(ctx, child, cfg, child.At))
	require.NoError(t, manager.Tick(ctx, cfg, child.At, false))
	saved, err := s.Incident(ctx, parent.Fingerprint+":v1")
	require.NoError(t, err)
	require.Equal(t, "firing", saved.Status)
	require.NoError(t, manager.Tick(ctx, cfg, child.At, true))
	saved, err = s.Incident(ctx, parent.Fingerprint+":v1")
	require.NoError(t, err)
	require.Equal(t, "resolved", saved.Status)
}

type localNotificationTransport struct{ target *url.URL }

func (r localNotificationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	u.Scheme = r.target.Scheme
	u.Host = r.target.Host
	copy.URL = &u
	return http.DefaultTransport.RoundTrip(copy)
}
