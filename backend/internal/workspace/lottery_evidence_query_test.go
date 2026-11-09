package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLotteryEvidenceQuerySelectsOnlyRequiredSources(t *testing.T) {
	for _, tc := range []struct {
		name       string
		conditions []lotteryCondition
		sources    []string
		absent     []string
	}{
		{"all-users", nil, nil, []string{"usage_logs", "redeem_codes", "payment_orders"}},
		{"balance", []lotteryCondition{{Kind: "balance_min", Value: "0"}}, nil, []string{"usage_logs", "redeem_codes", "payment_orders"}},
		{"daily-spend", []lotteryCondition{{Kind: "spend_today", Value: "0"}}, []string{"usage_logs"}, []string{"redeem_codes", "payment_orders", "SUM(amount) AS total"}},
		{"total-spend", []lotteryCondition{{Kind: "spend_total", Value: "0"}}, []string{"usage_logs"}, []string{"redeem_codes", "payment_orders", "AS today"}},
		{"daily-recharge", []lotteryCondition{{Kind: "recharge_today", Value: "0"}}, []string{"redeem_codes", "payment_orders"}, []string{"usage_logs", "SUM(amount) AS total"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validLotterySpec()
			s.Conditions = tc.conditions
			q := lotteryEvidenceQuery(s, lotteryEvidencePreview)
			for _, source := range tc.sources {
				if !strings.Contains(q, source) {
					t.Fatalf("missing source %s", source)
				}
			}
			for _, source := range tc.absent {
				if strings.Contains(q, source) {
					t.Fatalf("unrequested source %s", source)
				}
			}
			if strings.Count(q, "FROM usage_logs") > 1 {
				t.Fatal("repeated usage scans")
			}
			if !strings.Contains(q, "LIMIT 100001") {
				t.Fatal("preview candidate guard missing")
			}
		})
	}
	s := validLotterySpec()
	s.Conditions = []lotteryCondition{{Kind: "spend_today", Value: "0"}, {Kind: "spend_total", Value: "0"}}
	q := lotteryEvidenceQuery(s, lotteryEvidencePreview)
	if strings.Count(q, "FROM usage_logs") != 1 || !strings.Contains(q, "SUM(amount) FILTER") || !strings.Contains(q, "SUM(amount) AS total") {
		t.Fatal("daily and lifetime facts must share one source aggregation")
	}
}

func TestLotteryPreviewPropagatesReadFailureAndSkipsUnusedFacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := validLotterySpec()
	s.Conditions = nil
	row := sqlmock.NewRows([]string{"id", "balance", "created_at", "spend_today", "spend_total", "recharge_today", "recharge_total"}).AddRow(7, "0", time.Now(), "", "", "", "").AddRow(8, "0", time.Now(), "", "", "", "").RowError(1, fmt.Errorf("read interrupted"))
	mock.ExpectQuery("WITH lottery_bounds").WillReturnRows(row)
	_, _, err = (&Handler{db: db}).lotteryPreview(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "read interrupted") {
		t.Fatal("partial preview must fail")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLotteryEvidenceKeepsLegacySignatureEncoding(t *testing.T) {
	raw := `{"user_id":7,"balance":"3.00","registered_at":"2026-10-25T00:00:00Z","spend_today":"1.25","spend_total":"50","recharge_today":"0","recharge_total":"100","business_date":"2026-10-25"}`
	var e lotteryEvidence
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(e)
	if err != nil || string(encoded) != raw {
		t.Fatal("legacy evidence encoding changed")
	}
	// Empty unrequested facts also remain round-trippable by previous versions.
	e.SpendToday = ""
	encoded, _ = json.Marshal(e)
	if !strings.Contains(string(encoded), `"spend_today":""`) {
		t.Fatal("signature shape changed")
	}
}
