package workspace

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func futureEditableLottery() lotteryActivity {
	spec := validLotterySpec()
	spec.StartAt = time.Now().Add(-time.Hour).Truncate(time.Second)
	spec.DrawMode, spec.DrawAt = "full", nil
	return lotteryActivity{ID: 11, CreatedBy: 3, Status: "open", Spec: spec}
}
func copyLotterySpec(spec lotterySpec) lotterySpec {
	raw, _ := json.Marshal(spec)
	var result lotterySpec
	_ = json.Unmarshal(raw, &result)
	return result
}

func TestLotteryPublishedAmendmentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries int
		change  func(*lotteryActivity, *lotterySpec)
		reject  bool
	}{
		{"add_deadline", 1, func(a *lotteryActivity, s *lotterySpec) {
			d := time.Now().Add(time.Hour)
			s.DrawMode = "either"
			s.DrawAt = &d
		}, false},
		{"title", 1, func(a *lotteryActivity, s *lotterySpec) { s.Title = "Updated" }, false},
		{"more_prizes", 1, func(a *lotteryActivity, s *lotterySpec) { s.Prizes[0].Amount = "2"; s.Prizes[0].Count = 3 }, false},
		{"future_start_without_entries", 0, func(a *lotteryActivity, s *lotterySpec) { s.StartAt = time.Now().Add(time.Hour) }, false},
		{"capacity_below_entries", 2, func(a *lotteryActivity, s *lotterySpec) { s.Capacity = 1 }, true},
		{"reduce_amount", 1, func(a *lotteryActivity, s *lotterySpec) { s.Prizes[0].Amount = "0.01" }, true},
		{"reduce_prize_count", 1, func(a *lotteryActivity, s *lotterySpec) { s.Prizes[0].Count = 1 }, true},
		{"remove_prize", 1, func(a *lotteryActivity, s *lotterySpec) {
			s.Prizes = []lotteryPrize{{Key: "second", Amount: "1", Count: 1}}
		}, true},
		{"change_snapshot_day", 1, func(a *lotteryActivity, s *lotterySpec) { s.BusinessDate = "2026-10-26" }, true},
		{"change_start", 1, func(a *lotteryActivity, s *lotterySpec) { s.StartAt = s.StartAt.Add(time.Minute) }, true},
		{"past_deadline", 1, func(a *lotteryActivity, s *lotterySpec) {
			d := time.Now().Add(-time.Minute)
			s.DrawMode = "either"
			s.DrawAt = &d
		}, true},
		{"already_due", 3, func(a *lotteryActivity, s *lotterySpec) { s.Capacity = 4 }, true},
		{"drawn", 1, func(a *lotteryActivity, s *lotterySpec) { a.Status = "drawn" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := futureEditableLottery()
			next := copyLotterySpec(old.Spec)
			tc.change(&old, &next)
			err := validateLotteryAmendment(old, next, tc.entries, time.Now())
			if (err != nil) != tc.reject {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestLotteryPublishedSaveLocksAndChecksRevision(t *testing.T) {
	for _, scenario := range []string{"success", "stale", "missing_revision", "drawn", "audit_failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			key := bytes.Repeat([]byte{7}, 32)
			old := futureEditableLottery()
			if scenario == "drawn" {
				old.Status = "drawn"
			}
			old.Signature = lotterySpecSignature(key, old)
			next := copyLotterySpec(old.Spec)
			next.Title = "New title"
			_ = normalizeLotterySpec(&next)
			record, _ := json.Marshal(map[string]string{"encrypted_key": hex.EncodeToString(key)})
			mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("lottery_signing_key").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(record))
			mock.ExpectBegin()
			raw, _ := json.Marshal(old.Spec)
			mock.ExpectQuery("SELECT .* FROM workspace_lotteries WHERE id=\\$1 FOR UPDATE").WithArgs(old.ID).WillReturnRows(sqlmock.NewRows(strings.Split(lotteryColumns, ",")).AddRow(old.ID, raw, old.Signature, old.Status, old.CreatedBy, time.Now(), time.Now(), nil, 0, ""))
			revision := lotteryRevision(old)
			if scenario == "missing_revision" {
				revision = ""
			}
			if scenario == "stale" {
				revision = strings.Repeat("0", 64)
			}
			success := scenario == "success"
			if success || scenario == "audit_failure" {
				mock.ExpectQuery("SELECT count.* FROM workspace_lottery_entries").WithArgs(old.ID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				mock.ExpectExec("UPDATE workspace_lotteries SET spec").WillReturnResult(sqlmock.NewResult(0, 1))
				audit := mock.ExpectExec("INSERT INTO workspace_lottery_events")
				if scenario == "audit_failure" {
					audit.WillReturnError(errors.New("audit unavailable"))
				} else {
					audit.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			if success {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			item, err := (&Handler{db: db, encryptor: lotteryFixtureEncryptor{}}).saveLotteryActivity(context.Background(), actor{ID: 9, Admin: true}, old.ID, next, revision)
			if (err == nil) != success {
				t.Fatalf("unexpected save result: %v", err)
			}
			if success && (item.Status != "open" || item.ID != old.ID || item.CreatedBy != old.CreatedBy || !lotterySignatureOK(item.Signature, lotterySpecSignature(key, item))) {
				t.Fatal("published activity identity or signature changed incorrectly")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLotteryAmendmentPreservesSignedEntrantEligibility(t *testing.T) {
	for _, scenario := range []string{"broaden", "exclude", "tamper", "wrong_day"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			key := bytes.Repeat([]byte{8}, 32)
			old := futureEditableLottery()
			next := copyLotterySpec(old.Spec)
			evidence := lotteryEvidence{UserID: 8, BusinessDate: old.Spec.BusinessDate, SpendTotal: "5"}
			if scenario == "exclude" {
				next.Conditions = []lotteryCondition{{Kind: "spend_total", Value: "10"}}
			}
			if scenario == "wrong_day" {
				evidence.BusinessDate = "2026-10-26"
			}
			signature := lotterySign(key, "entry", []any{old.ID, int64(8), evidence})
			if scenario == "tamper" {
				signature = "broken"
			}
			raw, _ := json.Marshal(evidence)
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			mock.ExpectQuery("SELECT user_id,evidence,signature FROM workspace_lottery_entries").WithArgs(old.ID).WillReturnRows(sqlmock.NewRows([]string{"user_id", "evidence", "signature"}).AddRow(8, raw, signature))
			err = verifyLotteryAmendmentEntries(context.Background(), tx, old, next, key)
			if (err == nil) != (scenario == "broaden") {
				t.Fatalf("eligibility/snapshot error: %v", err)
			}
			mock.ExpectRollback()
			_ = tx.Rollback()
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestLotteryEditRejectsOrdinaryUserBeforeDatabase(t *testing.T) {
	if _, err := (&Handler{}).saveLotteryActivity(context.Background(), actor{ID: 8}, 11, validLotterySpec(), ""); err == nil {
		t.Fatal("ordinary user could edit")
	}
}
func TestTicketInboxScopePreservesOwnerAndParameterizedSearch(t *testing.T) {
	for _, admin := range []bool{false, true} {
		text := "9' OR TRUE --"
		where, args := ticketListScope(actor{ID: 8, Admin: admin}, "open", text, true)
		if !strings.Contains(where, "($1 OR t.user_id=$2)") || !strings.Contains(where, "unread.author_role<>$6") || strings.Contains(where, text) || len(args) != 6 || args[1] != int64(8) || args[3] != text || args[4] != true {
			t.Fatalf("unsafe inbox scope: %s %#v", where, args)
		}
		expected := "user"
		if admin {
			expected = "admin"
		}
		if args[5] != expected {
			t.Fatal("read direction crossed roles")
		}
	}
}
