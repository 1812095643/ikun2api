package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

type blindboxSettingsAdmin struct{ service.AdminService }

func (*blindboxSettingsAdmin) GetUserIdentity(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 8}, nil
}

func TestBlindboxConfigurableAllowanceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		step, spend string
		want        int
	}{
		{"10", "0", 0}, {"10", "0.0000000001", 1}, {"10", "10", 1},
		{"10", "10.0000000001", 2}, {"250", "250", 1}, {"250", "250.01", 2},
		{"0.00000001", "0.00000001", 1}, {"0.00000001", "0.0000000101", 2},
		{"1000000", "1000000.1", 2}, {"0.00000001", "1000000000000000000", blindboxCapacity},
	} {
		got, err := blindboxAllowance(tc.spend, tc.step)
		if err != nil || got != tc.want {
			t.Fatalf("spend=%s threshold=%s: got=%d err=%v", tc.spend, tc.step, got, err)
		}
	}
	for _, step := range []string{"", "0", "-1", "NaN", "Infinity", "1e2147483647", "1000000.00000001", "0.000000001", " 1", "1.2.3"} {
		if _, err := blindboxAllowance("1", step); err == nil {
			t.Fatalf("invalid threshold accepted: %q", step)
		}
	}
}

func TestBlindboxSettingsReadConfiguredThreshold(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT .*usage_per_chance::text FROM workspace_blindbox_settings.*FOR SHARE").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "price", "minimum", "maximum", "draw_time", "usage_per_chance"}).
			AddRow(true, "2.50000000", "1.00000000", "10.00000000", "21:00", "10.00000000"))
	s, err := loadBlindboxSettings(context.Background(), db, true)
	if err != nil || s.UsagePerChance != "10" {
		t.Fatalf("settings=%+v err=%v", s, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBlindboxAdminSettingsSaveAndLegacyOmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, field, stored, want string
		arg                       any
		code                      int
	}{
		{"custom", `,"usage_per_chance":"250.12500000"`, "250.12500000", "250.125", "250.125", 200},
		{"fractional", `,"usage_per_chance":"0.00000001"`, "0.00000001", "0.00000001", "0.00000001", 200},
		{"omitted_preserves_current", "", "10.00000000", "10", nil, 200},
		{"empty_rejected", `,"usage_per_chance":""`, "", "", nil, 400},
		{"zero_rejected", `,"usage_per_chance":"0"`, "", "", nil, 400},
		{"oversized_rejected", `,"usage_per_chance":"1000001"`, "", "", nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("workspace_admin", true)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 8})
			c.Request = httptest.NewRequest("PUT", "/admin/workspace/blindbox/settings", strings.NewReader(`{"enabled":true,"price":"2.5","minimum":"1","maximum":"10","draw_time":"21:00"`+tc.field+`}`))
			if tc.code == 200 {
				mock.ExpectQuery("UPDATE workspace_blindbox_settings SET .*usage_per_chance=COALESCE.* RETURNING usage_per_chance::text").
					WithArgs(true, "2.5", "1", "10", "21:00", int64(8), tc.arg).
					WillReturnRows(sqlmock.NewRows([]string{"usage_per_chance"}).AddRow(tc.stored))
			}
			(&Handler{db: db, admin: &blindboxSettingsAdmin{}}).saveBlindboxSettings(c)
			if recorder.Code != tc.code {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tc.code == 200 {
				var result struct {
					Data blindboxSettings `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Data.UsagePerChance != tc.want {
					t.Fatalf("saved threshold=%q", result.Data.UsagePerChance)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBlindboxSettingsDenyNonAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fn := range []func(*Handler, *gin.Context){(*Handler).getBlindboxSettings, (*Handler).saveBlindboxSettings} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		fn(&Handler{}, c)
		if r.Code != 404 {
			t.Fatalf("unauthorized settings status=%d", r.Code)
		}
	}
}

func TestBlindboxPurchaseUsesConfiguredThreshold(t *testing.T) {
	for _, tc := range []struct {
		step, spend  string
		bought, code int
	}{
		{"10", "10.0000000001", 1, 400}, // A second purchase reaches the balance check; the old 100 threshold would deny it first.
		{"250", "250", 1, 403},          // Higher thresholds must also constrain the actual purchase endpoint.
	} {
		t.Run(tc.step, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			key := bytes.Repeat([]byte{7}, 32)
			record, _ := json.Marshal(map[string]string{"encrypted_key": hex.EncodeToString(key)})
			mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("lottery_signing_key").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(record))
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM workspace_blindbox_entries").WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("SELECT .*usage_per_chance::text FROM workspace_blindbox_settings.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"enabled", "price", "minimum", "maximum", "draw_time", "usage_per_chance"}).AddRow(true, "2.5", "1", "10", "21:00", tc.step))
			now := time.Date(2026, 11, 15, 4, 0, 0, 0, time.UTC)
			mock.ExpectQuery("SELECT clock_timestamp").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			deadline, _ := blindboxDeadline("2026-11-15", testBlindboxSpec())
			round := blindboxRound{ID: 1, Date: "2026-11-15", Spec: testBlindboxSpec(), DrawAt: deadline, Status: "open", Count: 1, TotalPaid: "2.5", TotalAwarded: "0"}
			raw, _ := json.Marshal(round.Spec)
			roundRows := func() *sqlmock.Rows {
				return sqlmock.NewRows(strings.Split(blindboxRoundColumns, ",")).AddRow(round.ID, round.Date, raw, blindboxRoundSign(key, round), deadline, "open", 1, "2.5", "0", "", nil)
			}
			mock.ExpectQuery("SELECT .* FROM workspace_blindbox_rounds").WillReturnRows(roundRows())
			mock.ExpectQuery("SELECT .* FROM workspace_blindbox_rounds.*FOR UPDATE").WillReturnRows(roundRows())
			mock.ExpectQuery("SELECT balance::text FROM users.*FOR UPDATE").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("0"))
			mock.ExpectQuery("SELECT .* FROM workspace_blindbox_entries").WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("SELECT clock_timestamp").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			mock.ExpectQuery("SELECT EXISTS.* FROM workspace_checkins").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			mock.ExpectQuery(regexp.QuoteMeta(blindboxUsageSQL)).WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow(tc.spend))
			mock.ExpectQuery("SELECT count.* FROM workspace_blindbox_entries").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.bought))
			mock.ExpectRollback()
			h := &Handler{db: db, encryptor: lotteryFixtureEncryptor{}}
			_, _, err = h.purchaseBlindbox(context.Background(), 8, "test-request-identifier", blindboxQuote(key, round.Date, round.Spec))
			var rejected *requestError
			if !errors.As(err, &rejected) || rejected.status != tc.code {
				t.Fatalf("unexpected purchase error: %v", err)
			}
			if tc.code == 400 && !strings.Contains(rejected.message, "余额不足") {
				t.Fatalf("did not reach balance check: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
