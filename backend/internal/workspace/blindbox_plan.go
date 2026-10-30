package workspace

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/shopspring/decimal"
)

const blindboxCapacity = 10000

type blindboxTier struct {
	Amount      string `json:"amount"`
	Count       int    `json:"count"`
	Probability string `json:"probability"`
	Result      string `json:"result"`
}

type blindboxPlan struct {
	Rewards []int64        `json:"-"`
	Tiers   []blindboxTier `json:"tiers"`
	Total   string         `json:"total"`
	Even    int            `json:"even"`
	Lose    int            `json:"lose"`
	Win     int            `json:"win"`
	Refund  bool           `json:"refund"`
}

func blindboxMoney(cents int64) string { return decimal.New(cents, -2).String() }

func blindboxCents(raw string) (int64, error) {
	if !checkinMoneyPattern.MatchString(raw) {
		return 0, badRequest("盲盒金额请保留至多两位小数。")
	}
	v, err := decimal.NewFromString(raw)
	if err != nil || v.LessThan(decimal.NewFromFloat(0.01)) || v.GreaterThan(decimal.NewFromInt(1000)) || !v.Equal(v.Round(2)) {
		return 0, badRequest("盲盒金额须在 0.01 至 1000 之间，最多两位小数。")
	}
	return v.Shift(2).IntPart(), nil
}

// 在上下界内按权重分配整数分，每份至少一分；始终保留后续份数的容量，不使用浮点累计或随机补差。
func blindboxSpread(total int64, count int, capacity int64, weighted bool) ([]int64, error) {
	if count < 1 || total < int64(count) || total > int64(count)*capacity {
		return nil, fmt.Errorf("盲盒奖池分配边界不一致")
	}
	result := make([]int64, count)
	left := total - int64(count)
	var weights int64
	for i := 1; i <= count; i++ {
		w := int64(i)
		if weighted {
			w *= w
		}
		weights += w
	}
	for i := count - 1; i >= 0; i-- {
		w := int64(i + 1)
		if weighted {
			w *= w
		}
		numerator := new(big.Int).Mul(big.NewInt(left), big.NewInt(w))
		extra := numerator.Quo(numerator, big.NewInt(weights)).Int64()
		extra = max(max(int64(0), left-int64(i)*(capacity-1)), min(extra, capacity-1))
		result[i] = 1 + extra
		left -= extra
		weights -= w
	}
	if left != 0 {
		return nil, fmt.Errorf("盲盒奖池分配存在差额")
	}
	return result, nil
}

// 先生成可公开复算的奖金清单，再在开奖事务中用密码学随机源洗牌分配。清单金额总和严格等于购入金额。
func makeBlindboxPlan(count int, s blindboxSpec) (blindboxPlan, error) {
	plan := blindboxPlan{Rewards: []int64{}, Tiers: []blindboxTier{}, Total: "0"}
	if count < 0 || count > blindboxCapacity {
		return plan, fmt.Errorf("盲盒份数超出范围")
	}
	price, err := blindboxCents(s.Price)
	if err != nil {
		return plan, err
	}
	minimum, err := blindboxCents(s.Minimum)
	if err != nil {
		return plan, err
	}
	maximum, err := blindboxCents(s.Maximum)
	if err != nil {
		return plan, err
	}
	if minimum >= price || maximum <= price {
		return plan, fmt.Errorf("盲盒奖励上下限不包含售价")
	}
	plan.Total = blindboxMoney(int64(count) * price)
	if count == 0 {
		return plan, nil
	}
	if count == 1 {
		plan.Rewards = []int64{price}
		plan.Refund = true
		plan.Even = 1
	} else {
		even := min((count*30+50)/100, count-2)
		lose, win := 0, 0
		feasible := false
		// 极窄金额区间和奇数份数可能无法精确抵销一分钱，必要时将一个份额转为保本并公布实际比例。
		for attempt := 0; attempt < 2 && !feasible; attempt++ {
			remaining := count - even
			for delta := 0; delta < remaining; delta++ {
				for _, direction := range []int{-1, 1} {
					candidate := remaining/2 + direction*delta
					if candidate < 1 || candidate >= remaining {
						continue
					}
					l := remaining - candidate
					if max(int64(l), int64(candidate)) <= min(int64(l)*(price-minimum), int64(candidate)*(maximum-price)) {
						lose, win = l, candidate
						feasible = true
						break
					}
				}
				if feasible {
					break
				}
			}
			if !feasible {
				even++
			}
		}
		if !feasible {
			return plan, fmt.Errorf("盲盒奖金清单无法平衡")
		}
		upper := min(int64(lose)*(price-minimum), int64(win)*(maximum-price))
		transfer := max(max(int64(lose), int64(win)), upper*4/5)
		losses, err := blindboxSpread(transfer, lose, price-minimum, false)
		if err != nil {
			return plan, err
		}
		profits, err := blindboxSpread(transfer, win, maximum-price, true)
		if err != nil {
			return plan, err
		}
		for _, v := range losses {
			plan.Rewards = append(plan.Rewards, price-v)
		}
		for i := 0; i < even; i++ {
			plan.Rewards = append(plan.Rewards, price)
		}
		for _, v := range profits {
			plan.Rewards = append(plan.Rewards, price+v)
		}
		plan.Even, plan.Lose, plan.Win = even, lose, win
	}
	counts := map[int64]int{}
	var sum int64
	for _, reward := range plan.Rewards {
		if reward < minimum || reward > maximum {
			return plan, fmt.Errorf("盲盒奖金超出约定范围")
		}
		sum += reward
		counts[reward]++
	}
	if sum != int64(count)*price || len(plan.Rewards) != count {
		return plan, fmt.Errorf("盲盒奖池核算不一致")
	}
	amounts := make([]int64, 0, len(counts))
	for amount := range counts {
		amounts = append(amounts, amount)
	}
	sort.Slice(amounts, func(i, j int) bool { return amounts[i] < amounts[j] })
	for _, amount := range amounts {
		result := "even"
		if amount < price {
			result = "lose"
		} else if amount > price {
			result = "win"
		}
		probability := decimal.NewFromInt(int64(counts[amount])).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(int64(count))).StringFixed(2)
		plan.Tiers = append(plan.Tiers, blindboxTier{Amount: blindboxMoney(amount), Count: counts[amount], Probability: probability, Result: result})
	}
	return plan, nil
}
