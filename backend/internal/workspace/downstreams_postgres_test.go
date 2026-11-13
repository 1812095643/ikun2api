package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/pkg/adminaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func downstreamFixture(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("IKUN_BUSINESS_TEST_DSN")
	if dsn == "" {
		t.Skip("dedicated ikun_business_fixture PostgreSQL required")
	}
	base, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	var name string
	require.NoError(t, base.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "ikun_business_fixture", name)
	schema := "downstream_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, e)
	db, e := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, e)
	t.Cleanup(func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() })
	_, e = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,email text,username text DEFAULT '',status text DEFAULT 'active',balance numeric DEFAULT 0,updated_at timestamptz DEFAULT now(),deleted_at timestamptz);
 CREATE TABLE groups(id bigint PRIMARY KEY,name text,deleted_at timestamptz);
 CREATE TABLE user_group_rate_multipliers(user_id bigint,group_id bigint,rate_multiplier numeric(10,4),rpm_override integer,created_at timestamptz,updated_at timestamptz,PRIMARY KEY(user_id,group_id));
 CREATE TABLE redeem_codes(id bigint PRIMARY KEY,credit_source text DEFAULT 'legacy');CREATE TABLE channel_model_pricing(id bigint PRIMARY KEY);
 CREATE TABLE usage_logs(user_id bigint,group_id bigint,model text,input_tokens integer DEFAULT 0,output_tokens integer DEFAULT 0,cache_creation_tokens integer DEFAULT 0,cache_read_tokens integer DEFAULT 0,actual_cost numeric,created_at timestamptz DEFAULT NOW());
 INSERT INTO users(id,email,balance) VALUES(1,'admin@example.invalid',0),(8,'reseller@example.invalid',40),(9,'regular@example.invalid',20);
 INSERT INTO groups VALUES(10,'Claude',NULL),(11,'GPT',NULL);
 INSERT INTO user_group_rate_multipliers VALUES(8,10,0.8,25,NOW(),NOW()),(8,11,0.9,12,NOW(),NOW());`)
	require.NoError(t, e)
	migration, e := os.ReadFile("../../migrations/272_admin_receivables_downstreams.sql")
	require.NoError(t, e)
	_, e = db.Exec(string(migration))
	require.NoError(t, e)
	return db
}
func TestDownstreamPostgresGroupPricingAndAtomicBatch(t *testing.T) {
	db := downstreamFixture(t)
	h := &Handler{db: db}
	ctx := context.Background()
	enable := true
	req := downstreamApply{UserIDs: []int64{8}, IsDownstream: &enable, Key: uuid.NewString()}
	require.NoError(t, normalizeDownstreamApply(&req))
	n, e := h.applyDownstream(ctx, 1, req)
	require.NoError(t, e)
	require.Equal(t, 1, n)
	var rate string
	var rpm int
	require.NoError(t, db.QueryRow(`SELECT rate_multiplier::text,rpm_override FROM user_group_rate_multipliers WHERE user_id=8 AND group_id=10`).Scan(&rate, &rpm))
	require.Equal(t, "0.8000", rate)
	require.Equal(t, 25, rpm)
	// Updating one group preserves other group overrides and RPM limits.
	req.Key = uuid.NewString()
	req.IsDownstream = nil
	newRate := 0.4321
	req.GroupRates = map[int64]*float64{10: &newRate}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := h.applyDownstream(ctx, 1, req); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.NoError(t, db.QueryRow(`SELECT rate_multiplier::text,rpm_override FROM user_group_rate_multipliers WHERE user_id=8 AND group_id=10`).Scan(&rate, &rpm))
	require.Equal(t, "0.4321", rate)
	require.Equal(t, 25, rpm)
	require.NoError(t, db.QueryRow(`SELECT rate_multiplier::text,rpm_override FROM user_group_rate_multipliers WHERE user_id=8 AND group_id=11`).Scan(&rate, &rpm))
	require.Equal(t, "0.9000", rate)
	require.Equal(t, 12, rpm)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM admin_downstream_operations`).Scan(&count))
	require.Equal(t, 2, count)
	// A retry with the same operation id and different content is rejected.
	changed := req
	changed.UserIDs = []int64{9}
	_, e = h.applyDownstream(ctx, 1, changed)
	require.Error(t, e)
	// One invalid member must roll back the entire batch.
	value := 0.2
	invalid := downstreamApply{UserIDs: []int64{8, 9}, GroupRates: map[int64]*float64{10: &value}, Key: uuid.NewString()}
	_, e = h.applyDownstream(ctx, 1, invalid)
	require.Error(t, e)
	require.NoError(t, db.QueryRow(`SELECT rate_multiplier::text FROM user_group_rate_multipliers WHERE user_id=8 AND group_id=10`).Scan(&rate))
	require.Equal(t, "0.4321", rate)
	// Restore the default rate without losing the RPM override.
	clear := downstreamApply{UserIDs: []int64{8}, GroupRates: map[int64]*float64{10: nil}, Key: uuid.NewString()}
	_, e = h.applyDownstream(ctx, 1, clear)
	require.NoError(t, e)
	var isNull bool
	require.NoError(t, db.QueryRow(`SELECT rate_multiplier IS NULL,rpm_override FROM user_group_rate_multipliers WHERE user_id=8 AND group_id=10`).Scan(&isNull, &rpm))
	require.True(t, isNull)
	require.Equal(t, 25, rpm)
	disable := false
	_, e = h.applyDownstream(ctx, 1, downstreamApply{UserIDs: []int64{8}, IsDownstream: &disable, Key: uuid.NewString()})
	require.NoError(t, e)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM user_group_rate_multipliers WHERE user_id=8`).Scan(&count))
	require.Equal(t, 2, count)
}
func TestDownstreamPostgresQueriesScopeStatisticsAndReceivables(t *testing.T) {
	db := downstreamFixture(t)
	h := &Handler{db: db}
	_, e := db.Exec(`UPDATE users SET is_downstream=TRUE WHERE id=8;
 INSERT INTO usage_logs(user_id,group_id,model,input_tokens,output_tokens,actual_cost) VALUES(8,10,'claude-fixture',120,30,1.25),(9,10,'claude-fixture',900,100,99);
 INSERT INTO usage_logs(user_id,group_id,model,actual_cost,created_at) VALUES(8,10,'old-model',50,NOW()-INTERVAL '40 days');
 INSERT INTO redeem_codes(id) VALUES(1);
 INSERT INTO admin_receivable_entries(user_id,kind,amount,outstanding_after,credit_record_id,actor_id,operation_key) VALUES(8,'credit',10,10,1,1,'11111111-1111-4111-8111-111111111111');`)
	require.NoError(t, e)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.registerBusinessAdmin(r.Group("/admin/workspace"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin/workspace/downstreams", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	var payload struct {
		Data struct {
			Items []downstreamUser `json:"items"`
			Total int              `json:"total"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Data.Total)
	require.Len(t, payload.Data.Items, 1)
	require.Equal(t, int64(1), payload.Data.Items[0].Requests)
	require.Equal(t, "1.25", payload.Data.Items[0].Cost)
	require.Equal(t, int64(150), payload.Data.Items[0].Tokens)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin/workspace/receivables", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "reseller@example.invalid")
	require.NotContains(t, w.Body.String(), "regular@example.invalid")
	// Existing debts remain visible after the user is deleted.
	_, e = db.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=8`)
	require.NoError(t, e)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin/workspace/receivables", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"deleted":true`)
}
func TestBusinessAdminRejectsRestrictedAdminKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
	})
	h := &Handler{}
	h.registerBusinessAdmin(r.Group("/admin/workspace"))
	for _, path := range []string{"/admin/workspace/receivables", "/admin/workspace/downstreams", "/admin/workspace/downstreams/8/usage"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 403, w.Code)
	}
}
func TestDownstreamValidationRejectsSilentPriceRounding(t *testing.T) {
	require.False(t, validDownstreamRate(0.12345))
	require.False(t, validDownstreamRate(1e-100))
	require.False(t, validDownstreamRate(0.123400000001))
	require.True(t, validDownstreamRate(0.1234))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?from=2026-01-01T00:00:00Z&to=2025-01-01T00:00:00Z", nil)
	_, _, e := downstreamRange(c)
	require.Error(t, e)
}
