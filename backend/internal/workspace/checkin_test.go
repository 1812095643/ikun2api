package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func testCheckinSettings() checkinSettings {
	return checkinSettings{Enabled: true, Mode: "fixed", FixedAmount: "0.10", RandomMin: "0.01", RandomMax: "0.10"}
}

func TestCheckinSettingsRejectUnsafeAmounts(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "NaN", "Infinity", "1e2147483647", "1000000.00000001", "0.000000001", " 1", "1.2.3"} {
		t.Run(value, func(t *testing.T) {
			s := testCheckinSettings()
			s.FixedAmount = value
			if normalizeCheckinSettings(&s) == nil {
				t.Fatalf("accepted %q", value)
			}
		})
	}
	s := testCheckinSettings()
	s.RandomMin, s.RandomMax = "2", "1"
	if normalizeCheckinSettings(&s) == nil {
		t.Fatal("accepted reversed range")
	}
	s = testCheckinSettings()
	s.Mode = "client"
	if normalizeCheckinSettings(&s) == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestCheckinRewardPrecisionAndBounds(t *testing.T) {
	s := testCheckinSettings()
	amount, low, high, err := checkinReward(s)
	if err != nil || amount != "0.1" || low != amount || high != amount {
		t.Fatalf("fixed reward changed: %s %s %s %v", amount, low, high, err)
	}
	for _, bounds := range [][2]string{{"0.01", "0.1"}, {"0.00000001", "0.00000009"}, {"0.001", "0.005"}, {"1", "1"}, {"1000000", "1000000"}} {
		s.Mode, s.RandomMin, s.RandomMax = "random", bounds[0], bounds[1]
		minimum, _ := decimal.NewFromString(bounds[0])
		maximum, _ := decimal.NewFromString(bounds[1])
		for i := 0; i < 50; i++ {
			amount, _, _, err = checkinReward(s)
			if err != nil {
				t.Fatal(err)
			}
			value, _ := decimal.NewFromString(amount)
			if value.LessThan(minimum) || value.GreaterThan(maximum) {
				t.Fatalf("reward outside bounds: %s", amount)
			}
			if bounds[0] == "0.01" && value.Exponent() < -2 {
				t.Fatalf("unexpected fraction of a cent: %s", amount)
			}
		}
	}
}

func TestCheckinBeijingMidnightAndStreak(t *testing.T) {
	before := time.Date(2026, 10, 26, 15, 59, 59, 0, time.UTC)
	if checkinDay(before) != "2026-10-26" || checkinDay(before.Add(time.Second)) != "2026-10-27" {
		t.Fatal("incorrect Beijing midnight")
	}
	for _, item := range []struct {
		last string
		want int
	}{{"2026-10-27", 8}, {"2026-10-26", 8}, {"2026-10-25", 0}, {"", 0}} {
		if got := checkinStreak(checkinRecord{Day: item.last, ConsecutiveDays: 8}, "2026-10-27"); got != item.want {
			t.Fatalf("streak for %s: %d", item.last, got)
		}
	}
}

func TestCheckinBodyRejectsClientRewardOrIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, payload := range []string{`{"amount":"999"}`, `{"user_id":1}`, `{"day":"2027-01-01"}`, `{} {}`, `{"nested":{}}`, strings.Repeat("x", 3000)} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(payload))
		if decodeCheckinBody(c, &struct{}{}) == nil {
			t.Fatalf("accepted client input: %s", payload)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	if err := decodeCheckinBody(c, &struct{}{}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinAdminDeniesUsersBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, handler := range []func(*Handler, *gin.Context){(*Handler).getCheckinSettings, (*Handler).saveCheckinSettings} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		handler(&Handler{}, c)
		if r.Code != 404 {
			t.Fatalf("admin endpoint returned %d", r.Code)
		}
	}
}

func checkinTestRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "day", "amount", "streak", "mode", "created_at"})
}

func expectCheckinStart(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM users.*FOR UPDATE").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(8))
	mock.ExpectQuery("SELECT clock_timestamp").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Date(2026, 10, 26, 16, 0, 1, 0, time.UTC)))
}

func TestCheckinDuplicateReturnsOriginalWithoutCrediting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectCheckinStart(mock)
	mock.ExpectQuery("SELECT .* FROM workspace_checkins WHERE user_id=.*checkin_day=").WithArgs(int64(8), "2026-10-27").WillReturnRows(checkinTestRow().AddRow(1, "2026-10-27", "0.10000000", 2, "fixed", time.Now()))
	mock.ExpectCommit()
	item, duplicate, err := (&Handler{db: db}).creditCheckin(context.Background(), 8)
	if err != nil || !duplicate || item.Amount != "0.1" {
		t.Fatalf("duplicate: %+v %v %v", item, duplicate, err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinDisabledRollsBackWithoutCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectCheckinStart(mock)
	mock.ExpectQuery("SELECT .* FROM workspace_checkins WHERE user_id=.*checkin_day=").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT .* FROM workspace_checkin_settings.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"enabled", "mode", "fixed", "min", "max"}).AddRow(false, "fixed", "0.1", "0.01", "0.1"))
	mock.ExpectRollback()
	_, _, err = (&Handler{db: db}).creditCheckin(context.Background(), 8)
	var denied *requestError
	if !errors.As(err, &denied) || denied.status != 403 {
		t.Fatalf("disabled reward: %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinLedgerAndRecordAreAtomic(t *testing.T) {
	for _, failAt := range []string{"", "ledger", "record"} {
		t.Run(failAt, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expectCheckinStart(mock)
			mock.ExpectQuery("SELECT .* FROM workspace_checkins WHERE user_id=.*checkin_day=").WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("SELECT .* FROM workspace_checkin_settings.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"enabled", "mode", "fixed", "min", "max"}).AddRow(true, "fixed", "0.1", "0.01", "0.1"))
			mock.ExpectQuery("SELECT .* FROM workspace_checkins WHERE user_id=.*ORDER BY").WillReturnRows(checkinTestRow().AddRow(1, "2026-10-26", "0.1", 3, "fixed", time.Now()))
			mock.ExpectQuery("UPDATE users SET balance=balance").WithArgs("0.1", int64(8)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1.1))
			ledger := mock.ExpectQuery("INSERT INTO redeem_codes").WithArgs("CHECKIN-8-20261027", "0.1", int64(8), "[赠送] 每日签到 2026-10-27", "gift")
			if failAt == "ledger" {
				ledger.WillReturnError(errors.New("ledger write interrupted"))
			} else {
				ledger.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
				record := mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO workspace_checkins")).WithArgs(int64(8), "2026-10-27", "0.1", 4, "fixed", "0.1", "0.1", int64(21), 1.1)
				if failAt == "record" {
					record.WillReturnError(errors.New("record write interrupted"))
				} else {
					record.WillReturnRows(checkinTestRow().AddRow(2, "2026-10-27", "0.1", 4, "fixed", time.Now()))
				}
			}
			if failAt == "" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			item, duplicate, err := (&Handler{db: db}).creditCheckin(context.Background(), 8)
			if failAt == "" && (err != nil || duplicate || item.ConsecutiveDays != 4) {
				t.Fatalf("credit: %+v %v %v", item, duplicate, err)
			}
			if failAt != "" && err == nil {
				t.Fatal("partial credit committed")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
