package workspace

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestBlindboxCurrencyScalePreservesAllowance(t *testing.T) {
	for _, spend := range []string{"0", "0.0000000001", "99.99", "100", "100.0000000001", "200", "200.01", "1000001"} {
		old, err := blindboxAllowance(spend, "100")
		if err != nil {
			t.Fatal(err)
		}
		v, _ := decimal.NewFromString(spend)
		got, err := blindboxAllowance(v.Div(decimal.NewFromInt(10)).String(), "10")
		if err != nil || got != old {
			t.Fatalf("%s: before=%d after=%d error=%v", spend, old, got, err)
		}
	}
	for _, step := range []string{"0", "-1", "NaN", "1000001", "1.000000001"} {
		if _, err := blindboxAllowance("100", step); err == nil {
			t.Fatalf("invalid step accepted: %s", step)
		}
	}
}

func TestBlindboxCurrencyScaleKeepsPoolBalanced(t *testing.T) {
	before := blindboxSpec{Price: "25", Minimum: "10", Maximum: "100", DrawTime: "21:00"}
	after := blindboxSpec{Price: "2.5", Minimum: "1", Maximum: "10", DrawTime: "21:00"}
	for _, count := range []int{0, 1, 2, 8, 61, 116, 10000} {
		a, err := makeBlindboxPlan(count, before)
		if err != nil {
			t.Fatal(err)
		}
		b, err := makeBlindboxPlan(count, after)
		if err != nil {
			t.Fatal(err)
		}
		av, _ := decimal.NewFromString(a.Total)
		bv, _ := decimal.NewFromString(b.Total)
		if !av.Div(decimal.NewFromInt(10)).Equal(bv) {
			t.Fatalf("pool mismatch for %d", count)
		}
		var sum int64
		for _, reward := range b.Rewards {
			sum += reward
			if reward < 100 || reward > 1000 {
				t.Fatalf("reward outside range: %d", reward)
			}
		}
		if blindboxMoney(sum) != b.Total {
			t.Fatalf("pool not balanced for %d", count)
		}
	}
}
