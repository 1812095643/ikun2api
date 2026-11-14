package workspace

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func TestUpstreamBalanceDisplayValidation(t *testing.T) {
	id := strings.Repeat("a", 64)
	d := defaultUpstreamBalanceDisplay()
	d.Order = []string{id, id}
	d.Hidden = []string{id, id}
	d.HiddenColumns = []string{"monitor", "monitor"}
	if err := normalizeUpstreamBalanceDisplay(&d); err != nil {
		t.Fatal(err)
	}
	if len(d.Order) != 1 || len(d.Hidden) != 1 || len(d.HiddenColumns) != 1 {
		t.Fatal("duplicates were not removed")
	}
	for _, mutate := range []func(*upstreamBalanceDisplay){
		func(d *upstreamBalanceDisplay) { d.Order = []string{"invalid"} },
		func(d *upstreamBalanceDisplay) { d.Hidden = []string{strings.Repeat("z", 64)} },
		func(d *upstreamBalanceDisplay) { d.HiddenColumns = []string{"actions"} },
		func(d *upstreamBalanceDisplay) { d.SortBy = "unknown" },
	} {
		next := defaultUpstreamBalanceDisplay()
		mutate(&next)
		if normalizeUpstreamBalanceDisplay(&next) == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}

func TestUpstreamBalanceDisplayIsScopedToAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, admin := range []bool{false, true} {
		for _, userID := range []int64{0, 8, 9} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("workspace_admin", admin)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
			key, err := upstreamBalanceDisplayKey(c)
			if !admin || userID == 0 {
				if err == nil {
					t.Fatal("non-admin accepted")
				}
				continue
			}
			if err != nil || key != "upstream_balance_display:"+map[int64]string{8: "8", 9: "9"}[userID] {
				t.Fatalf("key=%s err=%v", key, err)
			}
		}
	}
}

func TestUpstreamBalanceDisplaySaveDoesNotTouchMonitorOrSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := defaultUpstreamBalanceDisplay()
	d.Hidden = []string{strings.Repeat("a", 64)}
	d.Order = []string{strings.Repeat("b", 64), strings.Repeat("a", 64)}
	d.HiddenColumns = []string{"comparison"}
	raw, _ := json.Marshal(d)
	mock.ExpectExec("INSERT INTO workspace_runtime").WithArgs("upstream_balance_display:8", string(raw)).WillReturnResult(sqlmock.NewResult(0, 1))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("workspace_admin", true)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 8})
	c.Request = httptest.NewRequest("PUT", "/admin/workspace/upstream-balances/display", strings.NewReader(string(raw)))
	h := &Handler{db: db}
	h.saveUpstreamBalanceDisplay(c)
	if c.Writer.Status() != 200 {
		t.Fatalf("save status=%d", c.Writer.Status())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	// Each admin reads from a separate persisted key; no token or monitor fields are included.
	mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("upstream_balance_display:8").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
	loaded := defaultUpstreamBalanceDisplay()
	if err := h.getRuntime(context.Background(), "upstream_balance_display:8", &loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Hidden) != 1 || loaded.Hidden[0] != d.Hidden[0] {
		t.Fatal("display settings not persisted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamBalanceEmptySelectionDoesNotQueryAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest("POST", "/admin/workspace/upstream-balances/refresh", strings.NewReader(`{"ids":[]}`))
	(&Handler{}).refreshUpstreamBalances(c)
	if r.Code != 202 || !strings.Contains(r.Body.String(), `"queued":0`) {
		t.Fatalf("empty selection expanded to full query: %d %s", r.Code, r.Body.String())
	}
}
