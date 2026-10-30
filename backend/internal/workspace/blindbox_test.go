package workspace

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func testBlindboxSpec() blindboxSpec {
	return blindboxSpec{Price: "2.5", Minimum: "1", Maximum: "10", DrawTime: "21:00"}
}

func TestBlindboxPlanConservesPoolAndHasNoZeroRewards(t *testing.T) {
	for _, spec := range []blindboxSpec{testBlindboxSpec(), {Price: "0.02", Minimum: "0.01", Maximum: "0.03", DrawTime: "21:00"}, {Price: "0.02", Minimum: "0.01", Maximum: "1000", DrawTime: "21:00"}, {Price: "999.99", Minimum: "0.01", Maximum: "1000", DrawTime: "21:00"}} {
		for _, count := range []int{0, 1, 2, 3, 4, 5, 7, 10, 11, 99, 100, 127, 1000, 10000} {
			plan, err := makeBlindboxPlan(count, spec)
			if err != nil {
				t.Fatalf("%+v %d: %v", spec, count, err)
			}
			var sum int64
			price, _ := blindboxCents(spec.Price)
			minimum, _ := blindboxCents(spec.Minimum)
			maximum, _ := blindboxCents(spec.Maximum)
			for _, amount := range plan.Rewards {
				if amount < minimum || amount > maximum {
					t.Fatalf("out of bounds %d", amount)
				}
				sum += amount
			}
			if sum != int64(count)*price || len(plan.Rewards) != count {
				t.Fatalf("unbalanced %d %d", sum, count)
			}
			if count > 1 && (plan.Win < 1 || plan.Lose < 1) {
				t.Fatal("missing win or loss category")
			}
			if count > 1 && plan.Even+plan.Win+plan.Lose != count {
				t.Fatal("category counts mismatch")
			}
		}
	}
	plan, err := makeBlindboxPlan(100, testBlindboxSpec())
	if err != nil || plan.Even != 30 {
		t.Fatalf("expected 30 percent break even: %+v %v", plan, err)
	}
	if _, err = makeBlindboxPlan(blindboxCapacity+1, testBlindboxSpec()); err == nil {
		t.Fatal("unbounded allocation accepted")
	}
}

func TestBlindboxPublishedTiersMatchActualRewards(t *testing.T) {
	plan, err := makeBlindboxPlan(100, testBlindboxSpec())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int64]int{}
	for _, v := range plan.Rewards {
		counts[v]++
	}
	var total decimal.Decimal
	for _, tier := range plan.Tiers {
		amount, _ := blindboxCents(tier.Amount)
		if counts[amount] != tier.Count {
			t.Fatal("published tier count differs")
		}
		total = total.Add(decimal.RequireFromString(tier.Amount).Mul(decimal.NewFromInt(int64(tier.Count))))
	}
	if !total.Equal(decimal.RequireFromString(plan.Total)) {
		t.Fatal("published prize total differs")
	}
	copyRewards := append([]int64{}, plan.Rewards...)
	if err = shuffleBlindbox(copyRewards); err != nil {
		t.Fatal(err)
	}
	for _, v := range copyRewards {
		counts[v]--
	}
	for _, n := range counts {
		if n != 0 {
			t.Fatal("shuffle changed prizes")
		}
	}
}

func TestBlindboxQuotaUsesExactHundredBoundaries(t *testing.T) {
	for _, item := range []struct {
		amount string
		want   int
	}{{"0", 0}, {"0.0000000001", 1}, {"99.99", 1}, {"100", 1}, {"100.0000000001", 2}, {"200", 2}, {"200.01", 3}} {
		got, err := blindboxAllowance(item.amount)
		if err != nil || got != item.want {
			t.Fatalf("%s: %d %v", item.amount, got, err)
		}
	}
}

func TestBlindboxSignaturesBindOwnerLedgerAndAmounts(t *testing.T) {
	key := []byte("test-key-material")
	e := blindboxEntry{ID: 7, RoundID: 2, UserID: 9, Ordinal: 1, Paid: "2.5", Usage: "1", DebitID: 3}
	e.Signature = blindboxEntrySign(key, e)
	if verifyBlindboxEntry(e, key) != nil {
		t.Fatal("valid entry rejected")
	}
	for _, mutate := range []func(*blindboxEntry){func(e *blindboxEntry) { e.UserID++ }, func(e *blindboxEntry) { e.Paid = "10" }, func(e *blindboxEntry) { e.DebitID++ }, func(e *blindboxEntry) { e.Ordinal++ }} {
		copy := e
		mutate(&copy)
		if verifyBlindboxEntry(copy, key) == nil {
			t.Fatal("tampered purchase accepted")
		}
	}
	reward := "5"
	creditID := int64(8)
	signature := blindboxRewardSign(key, e, reward, creditID)
	e.Reward, e.CreditID, e.RewardSignature = &reward, &creditID, &signature
	if verifyBlindboxEntry(e, key) != nil {
		t.Fatal("valid reward rejected")
	}
	reward = "500"
	if verifyBlindboxEntry(e, key) == nil {
		t.Fatal("tampered reward accepted")
	}
	r := blindboxRound{ID: 1, Date: "2026-10-26", Spec: testBlindboxSpec(), DrawAt: time.Now()}
	signature = blindboxRoundSign(key, r)
	r.Spec.Maximum = "100"
	if lotterySignatureOK(signature, blindboxRoundSign(key, r)) {
		t.Fatal("modified rules retained signature")
	}
}
