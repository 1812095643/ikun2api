package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type toolsFixtureAdmin struct {
	service.AdminService
	accounts []service.Account
}

func (a *toolsFixtureAdmin) GetUser(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 1, Email: "admin@example.test", Username: "fixture"}, nil
}
func (a *toolsFixtureAdmin) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for _, x := range a.accounts {
		if x.ID == id {
			v := x
			return &v, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (a *toolsFixtureAdmin) ListAccounts(_ context.Context, page, size int, _, _, _, _ string, _ int64, _, _, _ string) ([]service.Account, int64, error) {
	if page > 1 {
		return nil, int64(len(a.accounts)), nil
	}
	return a.accounts, int64(len(a.accounts)), nil
}
func (a *toolsFixtureAdmin) GetAllGroupsByPlatform(context.Context, string) ([]service.Group, error) {
	return []service.Group{}, nil
}
func toolsPostgresFixture(t *testing.T) (*Handler, *gin.Engine) {
	t.Helper()
	dsn := os.Getenv("IKUN_TOOLS_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated local PostgreSQL not configured")
	}
	schema := "ikun_tools_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	base, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = base.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE"); base.Close() })
	if _, err = db.Exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY,name TEXT,platform TEXT,type TEXT,deleted_at TIMESTAMPTZ);
 CREATE TABLE workspace_runtime(key TEXT PRIMARY KEY,value JSONB NOT NULL,updated_at TIMESTAMPTZ DEFAULT NOW());
 INSERT INTO accounts VALUES(1,'a','anthropic','oauth',NULL),(2,'b','anthropic','oauth',NULL),(3,'c','anthropic','oauth',NULL),(4,'d','anthropic','oauth',NULL);`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE accounts ADD COLUMN status TEXT NOT NULL DEFAULT 'active', ADD COLUMN extra JSONB NOT NULL DEFAULT '{}', ADD COLUMN credentials JSONB NOT NULL DEFAULT '{}'`); err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.FS.ReadFile("263_admin_account_tools.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	v, _ := newVaultCipher(bytes.Repeat([]byte{3}, 32))
	h := &Handler{db: db, vault: v, admin: &toolsFixtureAdmin{}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("workspace_admin", true)
		c.Set("auth_method", "jwt")
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})
	// These endpoints deliberately bypass TOTP only in this isolated test router.
	// Production registration always applies vaultSensitive; its fail-closed path
	// is tested separately.
	router.GET("/vault", h.listVault)
	router.POST("/vault/preview", h.previewVault)
	router.POST("/vault/import", h.importVault)
	router.POST("/vault/delete", h.deleteVault)
	router.POST("/vault/:id/reveal", h.revealVault)
	router.PUT("/vault/:id", h.updateVault)
	router.GET("/warmup", h.getWarmup)
	router.GET("/runs", h.listWarmupRuns)
	return h, router
}
func toolsRequest(t *testing.T, r *gin.Engine, method, path string, p any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(p)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal("invalid fixture HTTP response")
	}
	return w.Code, body
}
func TestVaultPostgresImportLifecycle(t *testing.T) {
	h, r := toolsPostgresFixture(t)
	id := uuid.NewString()
	entry := vaultEntry{vaultSecret: vaultSecret{Email: "Case@example.com", Password: "TEST_ONLY-password", AuxiliaryEmail: "Backup@example.net", SessionKey: "sk-ant-sid02-TEST_ONLY"}, Platform: "claude", Title: "Fixture", Status: "unused"}
	batch := map[string]any{"batch_id": id, "items": []vaultEntry{entry}}
	for i := 0; i < 2; i++ {
		status, _ := toolsRequest(t, r, "POST", "/vault/import", batch)
		if status != 200 {
			t.Fatalf("import %d", status)
		}
	}
	var count int
	if err := h.db.QueryRow("SELECT count(*) FROM workspace_account_vault").Scan(&count); err != nil || count != 1 {
		t.Fatal("idempotency failed")
	}
	var rid, blob string
	h.db.QueryRow("SELECT id,secret_blob FROM workspace_account_vault").Scan(&rid, &blob)
	if strings.Contains(blob, "TEST_ONLY") || strings.Contains(blob, "Case@example.com") {
		t.Fatal("plaintext persisted")
	}
	status, body := toolsRequest(t, r, "GET", "/vault?platform=claude", nil)
	if status != 200 {
		t.Fatal("list failed")
	}
	wire, _ := json.Marshal(body)
	if bytes.Contains(wire, []byte("TEST_ONLY")) {
		t.Fatal("list exposed secret")
	}
	status, body = toolsRequest(t, r, "POST", "/vault/"+rid+"/reveal", map[string]string{"field": "session_key"})
	if status != 200 || body["data"].(map[string]any)["value"] != entry.SessionKey {
		t.Fatal("protected field roundtrip")
	}
	status, preview := toolsRequest(t, r, "POST", "/vault/preview", map[string]string{"raw": "Case@example.com----replacement----sk-ant-sid02-NEW--------"})
	if status != 200 {
		t.Fatal("preview failed")
	}
	raw, _ := json.Marshal(preview["data"])
	var parsed struct {
		Rows []vaultParseRow `json:"rows"`
	}
	_ = json.Unmarshal(raw, &parsed)
	row := parsed.Rows[0].Entry
	if row.ExistingID != rid || row.ExpectedVersion != 1 || row.Action != "skip" {
		t.Fatal("duplicate preview lost version")
	}
	row.Action = "replace"
	row.ExpectedVersion = 999
	status, _ = toolsRequest(t, r, "POST", "/vault/import", map[string]any{"batch_id": uuid.NewString(), "items": []vaultEntry{row}})
	if status != 409 {
		t.Fatal("stale import overwrote newer data")
	}
	row.ExpectedVersion = 1
	status, _ = toolsRequest(t, r, "POST", "/vault/import", map[string]any{"batch_id": uuid.NewString(), "items": []vaultEntry{row}})
	if status != 200 {
		t.Fatal("confirmed import failed")
	}
	status, body = toolsRequest(t, r, "POST", "/vault/"+rid+"/reveal", map[string]string{"field": "all"})
	if status != 200 {
		t.Fatal("reveal failed")
	}
	secret := body["data"].(map[string]any)["secret"].(map[string]any)
	if secret["password"] != "replacement" || secret["auxiliary_email"] != "Backup@example.net" {
		t.Fatal("nonempty update erased missing fields")
	}
	var audit int
	h.db.QueryRow("SELECT count(*) FROM workspace_vault_audit").Scan(&audit)
	if audit < 4 {
		t.Fatal("audit missing")
	}
	status, _ = toolsRequest(t, r, "POST", "/vault/delete", map[string]any{"ids": []string{rid}, "confirm": true})
	if status != 200 {
		t.Fatal("delete failed")
	}
	h.db.QueryRow("SELECT count(*) FROM workspace_account_vault").Scan(&count)
	if count != 0 {
		t.Fatal("delete incomplete")
	}
	h.db.QueryRow("SELECT count(*) FROM accounts").Scan(&count)
	if count != 4 {
		t.Fatal("vault deletion affected live accounts")
	}
	t.Log("real PostgreSQL: migration twice, cipher roundtrip, idempotency, stale conflict, duplicate fill, protected list, audit, isolated deletion")
}

type toolsWarmupExecutor struct {
	mu        sync.Mutex
	events    []string
	refreshes int
	sends     int
}

func (e *toolsWarmupExecutor) RefreshWarmupQuota(_ context.Context, _ *service.AccountUsageService, id int64) (*service.WarmupQuota, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshes++
	e.events = append(e.events, fmt.Sprintf("refresh:%d", id))
	if id == 4 {
		return nil, errors.New("offline")
	}
	q := &service.WarmupQuota{FiveHour: 0, SevenDay: 0, Source: "active", CheckedAt: time.Now()}
	if id == 2 {
		q.SevenDay = 99.1
	}
	return q, nil
}
func (e *toolsWarmupExecutor) RunAccountWarmup(ctx context.Context, id int64, _, _ string, _ int, send func() error) (service.WarmupAttemptResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, fmt.Sprintf("send:%d", id))
	if e.refreshes != 3 {
		return service.WarmupAttemptResult{}, errors.New("sending before refresh phase completed")
	}
	if err := send(); err != nil {
		return service.WarmupAttemptResult{}, err
	}
	e.sends++
	return service.WarmupAttemptResult{Status: "success", InputTokens: 4, OutputTokens: 2}, nil
}
func TestWarmupPostgresSerialPreflightAndSkip(t *testing.T) {
	h, _ := toolsPostgresFixture(t)
	now := time.Now()
	limit := now.Add(time.Hour)
	accounts := []service.Account{}
	for id := int64(1); id <= 4; id++ {
		a := service.Account{ID: id, Name: fmt.Sprint(id), Platform: "anthropic", Type: "oauth", Status: "active", Schedulable: true}
		if id == 3 {
			a.RateLimitedAt = &now
			a.RateLimitResetAt = &limit
		}
		accounts = append(accounts, a)
	}
	h.admin = &toolsFixtureAdmin{accounts: accounts}
	executor := &toolsWarmupExecutor{}
	h.warmupGateway = executor
	cfg := warmupConfig{Timezone: "Asia/Shanghai", Days: []int{0, 1, 2, 3, 4, 5, 6}, Windows: []warmupWindow{{Start: "00:00", End: "00:01", Rounds: 1}}, Model: "claude-test", Count: 1, MessageGap: 5, AccountGap: 5, Timeout: 10, DailyMax: 2}
	raw, _ := json.Marshal(cfg)
	if _, err := h.db.Exec("INSERT INTO workspace_warmup_rules(id,name,enabled,config) VALUES(1,'fixture',TRUE,$1)", raw); err != nil {
		t.Fatal(err)
	}
	if err := h.setRuntime(context.Background(), "account_warmup_settings", warmupSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 4; id++ {
		if _, err := h.db.Exec("INSERT INTO workspace_warmup_runs(rule_id,account_id,occurrence,planned_at,deadline) VALUES(1,$1,'manual/fixture',NOW(),NOW()+INTERVAL '5 minutes')", id); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := h.warmupTick(ctx); err != nil {
		t.Fatal(err)
	}
	if executor.sends != 1 || executor.refreshes != 3 {
		t.Fatalf("unexpected refresh/send counts: %d/%d", executor.refreshes, executor.sends)
	}
	var sent, skipped int
	h.db.QueryRow("SELECT count(*) FROM workspace_warmup_attempts").Scan(&sent)
	h.db.QueryRow("SELECT count(*) FROM workspace_warmup_runs WHERE status='skipped'").Scan(&skipped)
	if sent != 1 || skipped != 3 {
		t.Fatalf("attempts=%d skipped=%d", sent, skipped)
	}
	// A second tick cannot send a completed occurrence again.
	if err := h.warmupTick(ctx); err != nil {
		t.Fatal(err)
	}
	if executor.sends != 1 {
		t.Fatal("completed occurrence replayed")
	}
	var logs int
	h.db.QueryRow("SELECT count(*) FROM workspace_warmup_runs WHERE snapshot->>'quota_source'='active'").Scan(&logs)
	if logs != 2 {
		t.Fatal("active evidence missing")
	}
	t.Log("real PostgreSQL: serial active refresh before any sends; 429, >99%, query failures skipped; repeat tick sends zero requests")
}
