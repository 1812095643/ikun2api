package workspace

import "strings"

type lotteryEvidenceScope uint8

const (
	lotteryEvidenceUser lotteryEvidenceScope = iota
	lotteryEvidencePreview
	lotteryEvidenceEntrants
)

// Only collect monetary facts used by the rules. Uncollected facts are empty;
// evidence keeps its existing JSON shape so old and new signed snapshots remain
// verifiable across upgrades and rollbacks.
func lotteryEvidenceQuery(s lotterySpec, scope lotteryEvidenceScope) string {
	needed := make(map[string]bool, len(s.Conditions))
	for _, c := range s.Conditions {
		needed[c.Kind] = true
	}
	candidates := `SELECT u.id,u.balance,u.created_at FROM users u
WHERE u.deleted_at IS NULL AND u.status='active'`
	switch scope {
	case lotteryEvidencePreview:
		candidates += ` AND u.id >= $1 ORDER BY u.id LIMIT 100001`
	case lotteryEvidenceEntrants:
		candidates += ` AND u.id >= $1 AND EXISTS(SELECT 1 FROM workspace_lottery_entries e WHERE e.lottery_id=$4 AND e.user_id=u.id)`
	default:
		candidates += ` AND u.id=$1`
	}
	// Keep parameter types stable even when no date-dependent fact is requested.
	ctes := []string{`lottery_bounds AS (SELECT $2::timestamptz AS start_at,$3::timestamptz AS end_at)`,
		`lottery_candidates AS MATERIALIZED (` + candidates + `)`}
	columns := []string{`u.id`, `u.balance::text`, `u.created_at`}
	joins := []string{}
	spendToday, spendTotal := needed["spend_today"], needed["spend_total"]
	rechargeToday, rechargeTotal := needed["recharge_today"], needed["recharge_total"]

	if spendToday || spendTotal {
		source := `SELECT l.user_id,l.created_at AS booked_at,l.actual_cost AS amount
FROM usage_logs l JOIN lottery_candidates c ON c.id=l.user_id`
		if !spendTotal {
			source += ` WHERE l.created_at >= $2 AND l.created_at < $3`
		}
		ctes = append(ctes, lotteryAmountAggregate("lottery_spend", source, spendToday, spendTotal))
		joins = append(joins, `LEFT JOIN lottery_spend spend ON spend.user_id=u.id`)
	}
	if rechargeToday || rechargeTotal {
		// Count posted cash once: paid orders exclude their redeem rows. Gifts,
		// lottery winnings and ambiguous legacy credits remain excluded.
		redeem := `SELECT r.used_by AS user_id,r.used_at AS booked_at,r.value AS amount
FROM redeem_codes r JOIN lottery_candidates c ON c.id=r.used_by
WHERE r.status='used' AND r.value>0 AND r.type IN ('balance','admin_balance')
AND r.credit_source IN ('manual','redeem')
AND NOT EXISTS(SELECT 1 FROM payment_orders po WHERE po.recharge_code=r.code)`
		orders := `SELECT p.user_id,p.paid_at AS booked_at,p.amount
FROM payment_orders p JOIN lottery_candidates c ON c.id=p.user_id
WHERE p.order_type='balance' AND p.status='COMPLETED'`
		if !rechargeTotal {
			redeem += ` AND r.used_at >= $2 AND r.used_at < $3`
			orders += ` AND p.paid_at >= $2 AND p.paid_at < $3`
		}
		ctes = append(ctes, lotteryAmountAggregate("lottery_recharge", redeem+` UNION ALL `+orders, rechargeToday, rechargeTotal))
		joins = append(joins, `LEFT JOIN lottery_recharge recharge ON recharge.user_id=u.id`)
	}
	for _, metric := range []struct {
		alias        string
		today, total bool
	}{{"spend", spendToday, spendTotal}, {"recharge", rechargeToday, rechargeTotal}} {
		for _, value := range []struct {
			name   string
			needed bool
		}{{"today", metric.today}, {"total", metric.total}} {
			column := `''::text`
			if value.needed {
				column = `COALESCE(` + metric.alias + `.` + value.name + `,0)::text`
			}
			columns = append(columns, column)
		}
	}
	return `WITH ` + strings.Join(ctes, ",\n") + ` SELECT ` + strings.Join(columns, ",") +
		` FROM lottery_candidates u ` + strings.Join(joins, " ") + ` ORDER BY u.id`
}

// Aggregate each selected source once for the candidate set. Daily-only rules
// constrain the source by date before aggregation, so old usage is never read.
func lotteryAmountAggregate(name, source string, today, total bool) string {
	columns := []string{`user_id`}
	if today {
		columns = append(columns, `SUM(amount) FILTER (WHERE booked_at >= $2 AND booked_at < $3) AS today`)
	}
	if total {
		columns = append(columns, `SUM(amount) AS total`)
	}
	return name + ` AS (SELECT ` + strings.Join(columns, ",") + ` FROM (` + source + `) cash GROUP BY user_id)`
}
