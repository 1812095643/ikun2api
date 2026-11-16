package workspace

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
