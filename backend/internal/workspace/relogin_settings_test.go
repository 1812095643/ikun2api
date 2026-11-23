package workspace

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type reloginOptionsAdmin struct {
	service.AdminService
}

func (reloginOptionsAdmin) GetAllGroups(context.Context) ([]service.Group, error) {
	return []service.Group{{ID: 2, Name: "GPT", Platform: "openai", Status: "active", RateMultiplier: 0.7, AccountCount: 5}}, nil
}

func (reloginOptionsAdmin) GetAllProxies(context.Context) ([]service.Proxy, error) {
	expired := time.Now().Add(-time.Hour)
	return []service.Proxy{
		{ID: 3, Name: "默认代理", Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Username: "owner", Password: "private-proxy-password", Status: "active"},
		{ID: 4, Status: "inactive"},
		{ID: 5, Status: "active", ExpiresAt: &expired},
	}, nil
}

func TestReloginOptionsSupportsAccountSelectorsWithoutExposingProxyPassword(t *testing.T) {
	h := &Handler{admin: reloginOptionsAdmin{}}
	router := gin.New()
	router.GET("/options", h.reloginOptions)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/options", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		Data struct {
			Groups []struct {
				RateMultiplier float64 `json:"rate_multiplier"`
				AccountCount   int64   `json:"account_count"`
			} `json:"groups"`
			Proxies []struct {
				ID       int64  `json:"id"`
				Protocol string `json:"protocol"`
				Host     string `json:"host"`
				Port     int    `json:"port"`
			} `json:"proxies"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Data.Groups, 1)
	require.Equal(t, 0.7, result.Data.Groups[0].RateMultiplier)
	require.EqualValues(t, 5, result.Data.Groups[0].AccountCount)
	require.Len(t, result.Data.Proxies, 1)
	require.EqualValues(t, 3, result.Data.Proxies[0].ID)
	require.Equal(t, "socks5", result.Data.Proxies[0].Protocol)
	require.Equal(t, "127.0.0.1", result.Data.Proxies[0].Host)
	require.Equal(t, 1080, result.Data.Proxies[0].Port)
	require.NotContains(t, w.Body.String(), "private-proxy-password")
	require.NotContains(t, w.Body.String(), `"password"`)
}

type reloginSettingsAdmin struct {
	service.AdminService
	err error
}

func (a reloginSettingsAdmin) GetGroup(_ context.Context, id int64) (*service.Group, error) {
	if a.err != nil {
		return nil, a.err
	}
	if id == 82 {
		return nil, service.ErrGroupNotFound
	}
	return &service.Group{ID: id, Platform: "openai", Status: "active"}, nil
}

type reloginSavedGroups []int64

func (ids reloginSavedGroups) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var cfg reloginConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil || len(cfg.Template.GroupIDs) != len(ids) {
		return false
	}
	for i, id := range ids {
		if cfg.Template.GroupIDs[i] != id {
			return false
		}
	}
	return cfg.Template.LoadFactor != nil && *cfg.Template.LoadFactor == 10000 && cfg.Template.Extra.CodexFingerprintMode == "full"
}

func TestReloginSettingsSavePrunesOnlyPreviouslySavedUnavailableGroups(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   []int64
		groupErr error
		status   int
	}{
		{"清理已删除的旧分组", []int64{53, 82}, nil, 200},
		{"保存时再次清理刚删除的分组", []int64{53}, nil, 200},
		{"读取异常不冒充已删除", []int64{53, 82}, errors.New("database unavailable"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			cfg := defaultReloginConfig()
			cfg.Template.GroupIDs = tc.stored
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT value FROM workspace_runtime").WithArgs("relogin_settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
			mock.ExpectQuery("SELECT id FROM groups").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(53))
			if len(tc.stored) > 1 {
				mock.ExpectExec("UPDATE workspace_runtime").WithArgs("[53]", "[53,82]").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			cfg.Template.GroupIDs = []int64{53, 82, 53}
			payload, err := json.Marshal(cfg)
			require.NoError(t, err)
			if tc.status == 200 {
				mock.ExpectExec("INSERT INTO workspace_runtime").WithArgs("relogin_settings", reloginSavedGroups{53}).WillReturnResult(sqlmock.NewResult(1, 1))
			}
			h := &Handler{db: db, admin: reloginSettingsAdmin{err: tc.groupErr}}
			router := gin.New()
			router.PUT("/settings", h.saveReloginSettings)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("PUT", "/settings", strings.NewReader(string(payload))))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReloginSettingsPostgresPrunesDeletedGroupsWithoutOverwritingOtherChoices(t *testing.T) {
	h, _ := toolsPostgresFixture(t)
	_, err := h.db.Exec(`CREATE TABLE groups(id bigint PRIMARY KEY,deleted_at timestamptz,status text);
	 INSERT INTO groups VALUES(53,NULL,'active'),(54,NULL,'active'),(82,NOW(),'active'),(91,NULL,'disabled')`)
	require.NoError(t, err)
	cfg := defaultReloginConfig()
	cfg.Template.GroupIDs = []int64{53, 82, 91}
	require.NoError(t, h.setRuntime(context.Background(), "relogin_settings", cfg))
	_, err = h.db.Exec(`UPDATE workspace_runtime SET value=value || '{"future_setting":"keep"}'::jsonb`)
	require.NoError(t, err)
	loaded, err := h.loadReloginSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int64{53, 91}, loaded.Template.GroupIDs)
	var ids, future string
	require.NoError(t, h.db.QueryRow(`SELECT (value#>'{template,group_ids}')::text,value->>'future_setting' FROM workspace_runtime WHERE key='relogin_settings'`).Scan(&ids, &future))
	require.JSONEq(t, "[53,91]", ids)
	require.Equal(t, "keep", future)
	// 管理员先保存了新选择，旧读取不能覆盖新配置。
	_, err = h.db.Exec(`UPDATE workspace_runtime SET value=jsonb_set(value,'{template,group_ids}','[53,54]'::jsonb)`)
	require.NoError(t, err)
	cfg.Template.GroupIDs = []int64{53, 82, 91}
	require.NoError(t, h.pruneDeletedReloginGroups(context.Background(), &cfg))
	require.NoError(t, h.db.QueryRow(`SELECT (value#>'{template,group_ids}')::text FROM workspace_runtime WHERE key='relogin_settings'`).Scan(&ids))
	require.JSONEq(t, "[53,54]", ids)
}
