package workspace

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func validLotterySpec() lotterySpec {
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	end := now.Add(time.Hour)
	return lotterySpec{Title: "余额抽奖", BusinessDate: "2026-10-25", Match: "all", Conditions: []lotteryCondition{}, Prizes: []lotteryPrize{{Key: "first", Amount: "0.10000001", Count: 2}}, DrawMode: "either", Capacity: 3, StartAt: now, DrawAt: &end}
}

func TestLotteryRejectsInvalidMoneyAndCapacity(t *testing.T) {
	for _, amount := range []string{"NaN", "Infinity", "-1", "0", "0.000000001", "1000000.01", "1e308", "1e2147483647"} {
		s := validLotterySpec()
		s.Prizes[0].Amount = amount
		if normalizeLotterySpec(&s) == nil {
			t.Fatalf("accepted amount %q", amount)
		}
	}
	s := validLotterySpec()
	s.Capacity = 1
	if normalizeLotterySpec(&s) == nil {
		t.Fatal("prize count exceeds capacity")
	}
	s = validLotterySpec()
	s.Prizes = append(s.Prizes, s.Prizes[0])
	if normalizeLotterySpec(&s) == nil {
		t.Fatal("accepted duplicate prize")
	}
	s = validLotterySpec()
	s.Conditions = []lotteryCondition{{Kind: "user_ids", Value: "1);DELETE FROM users"}}
	if normalizeLotterySpec(&s) == nil {
		t.Fatal("accepted invalid user ids")
	}
}
func TestLotteryPrizePriorityAndExactBudget(t *testing.T) {
	s := validLotterySpec()
	s.Capacity = 0
	s.DrawMode = "time"
	s.Prizes = []lotteryPrize{{Key: "consolation", Amount: "0.1", Count: 3}, {Key: "special", Amount: "0.00000001", Count: 1}}
	if err := normalizeLotterySpec(&s); err != nil {
		t.Fatal(err)
	}
	total, count := lotteryTotals(s)
	if total != "0.30000001" || count != 4 || s.Prizes[0].Key != "special" {
		t.Fatalf("unexpected budget/order %s %d %+v", total, count, s.Prizes)
	}
}
func TestLotteryConditionsAndShanghaiBoundary(t *testing.T) {
	s := validLotterySpec()
	s.Conditions = []lotteryCondition{{Kind: "spend_today", Value: "1.00000001"}, {Kind: "registered_before", Value: "2026-10-25"}}
	e := lotteryEvidence{SpendToday: "1.00000002", RegisteredAt: time.Date(2026, 10, 24, 15, 59, 59, 0, time.UTC)}
	if !lotteryMatches(s, e) {
		t.Fatal("eligible user rejected")
	}
	e.SpendToday = "1.00000001"
	if lotteryMatches(s, e) {
		t.Fatal("greater-than accepted equality")
	}
	s.Match = "any"
	if !lotteryMatches(s, e) {
		t.Fatal("any condition rejected")
	}
	s.Match = "all"
	e.SpendToday = "2"
	e.RegisteredAt = time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)
	if lotteryMatches(s, e) {
		t.Fatal("Shanghai midnight boundary accepted")
	}
	start, end := lotteryDateBounds(s)
	if start.UTC().Format(time.RFC3339) != "2026-10-24T16:00:00Z" || end.Sub(start) != 24*time.Hour {
		t.Fatal("incorrect day bounds")
	}
}
func TestLotteryRulesRequireKnownCondition(t *testing.T) {
	s := validLotterySpec()
	s.Conditions = []lotteryCondition{{Kind: "admin", Value: "true"}}
	if normalizeLotterySpec(&s) == nil {
		t.Fatal("unknown condition accepted")
	}
}
func TestLotteryFullTimeEitherAndRepeatedDraw(t *testing.T) {
	s := validLotterySpec()
	a := lotteryActivity{Spec: s, Status: "open"}
	if lotteryDue(a, 2, s.StartAt) {
		t.Fatal("early draw")
	}
	if !lotteryDue(a, 3, s.StartAt) {
		t.Fatal("full draw missed")
	}
	a.Spec.DrawMode = "time"
	if lotteryDue(a, 3, s.StartAt) {
		t.Fatal("time-only drew when full")
	}
	if !lotteryDue(a, 0, *s.DrawAt) {
		t.Fatal("empty timed draw missed")
	}
	a.Status = "drawn"
	if lotteryDue(a, 3, *s.DrawAt) {
		t.Fatal("repeated draw")
	}
	a.Status = "open"
	a.Spec.DrawMode = "full"
	if lotteryDue(a, 2, s.DrawAt.Add(time.Hour)) {
		t.Fatal("full-only drew at deadline")
	}
}
func TestLotterySignatureDetectsOwnerMoneyAndRuleTampering(t *testing.T) {
	key := bytes.Repeat([]byte{19}, 32)
	a := lotteryActivity{ID: 11, CreatedBy: 9, Spec: validLotterySpec()}
	original := lotterySpecSignature(key, a)
	a.Spec.Prizes[0].Amount = "100"
	if lotterySignatureOK(original, lotterySpecSignature(key, a)) {
		t.Fatal("rule tampering undetected")
	}
	w := lotteryWin{LotteryID: 11, EntryID: 4, UserID: 2, PrizeKey: "first", Amount: "0.1"}
	signature := lotteryWinSignature(key, w)
	w.UserID = 3
	if lotterySignatureOK(signature, lotteryWinSignature(key, w)) {
		t.Fatal("owner tampering undetected")
	}
	w.UserID = 2
	w.Amount = "1"
	if lotterySignatureOK(signature, lotteryWinSignature(key, w)) {
		t.Fatal("amount tampering undetected")
	}
}

type lotteryBrokenReader struct{}

func (lotteryBrokenReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}
func TestLotteryShuffleIsPermutationAndFailsClosed(t *testing.T) {
	entries := []lotteryDrawEntry{{1, 2}, {2, 3}, {3, 4}, {4, 5}}
	if err := shuffleLottery(entries, bytes.NewReader(make([]byte, 128))); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, e := range entries {
		if seen[e.UserID] {
			t.Fatal("duplicate winner")
		}
		seen[e.UserID] = true
	}
	if len(seen) != 4 {
		t.Fatal("missing participant")
	}
	if shuffleLottery(entries, lotteryBrokenReader{}) == nil {
		t.Fatal("random failure ignored")
	}
}
func TestLotteryClaimBodyRejectsClientMoneyAndTrailingJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{`{"confirm":true,"amount":"1000"}`, `{"confirm":true,"user_id":2}`, `{"confirm":true} {"confirm":true}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/claim", strings.NewReader(raw))
		var input struct {
			Confirm bool `json:"confirm"`
		}
		if lotteryBody(c, &input) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
