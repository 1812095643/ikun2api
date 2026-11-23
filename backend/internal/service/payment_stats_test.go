//go:build unit

package service

import (
	"testing"
	"time"

	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/stretchr/testify/require"
)

func TestComputeBasicStatsGroupsAmountsByCurrency(t *testing.T) {
	t.Parallel()

	todayStart := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	yesterday := todayStart.Add(-time.Hour)
	today := todayStart.Add(time.Hour)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 10, &today),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &today),
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5, &yesterday),
	}

	stats := &DashboardStats{}
	computeBasicStats(stats, orders, todayStart)

	require.Equal(t, CurrencyAmounts{"CNY": 15, "USD": 10}, stats.TotalAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 10, "USD": 10}, stats.TodayAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 7.5, "USD": 10}, stats.AvgAmount)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, 2, stats.TodayCount)
}

func TestPaymentDashboardBreakdownsGroupAmountsAndRankingsByCurrency(t *testing.T) {
	t.Parallel()

	firstDay := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5.555, &firstDay),
		paymentStatsTestOrder(2, "bob@example.com", "CNY", 10, &firstDay),
		paymentStatsTestOrder(1, "alice@example.com", "USD", 20, &secondDay),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &secondDay),
	}
	orders[0].PaymentType = "stripe"
	orders[1].PaymentType = "stripe"
	orders[2].PaymentType = "stripe"
	orders[3].PaymentType = "alipay"

	daily := buildDailySeries(orders, firstDay.AddDate(0, 0, -1), 2)
	require.Equal(t, []DailyStats{
		{Date: "2026-07-24", Amount: CurrencyAmounts{"CNY": 15.56}, Count: 2},
		{Date: "2026-07-25", Amount: CurrencyAmounts{"USD": 30}, Count: 2},
	}, daily)

	methods := buildMethodDistribution(orders)
	require.Equal(t, []PaymentMethodStat{
		{Type: "alipay", Amount: CurrencyAmounts{"USD": 10}, Count: 1},
		{Type: "stripe", Amount: CurrencyAmounts{"CNY": 15.56, "USD": 20}, Count: 3},
	}, methods)

	users := buildTopUsers(orders)
	require.Equal(t, TopUsersByCurrency{
		"CNY": {
			{UserID: 2, Email: "bob@example.com", Amount: 10},
			{UserID: 1, Email: "alice@example.com", Amount: 5.56},
		},
		"USD": {
			{UserID: 1, Email: "alice@example.com", Amount: 20},
			{UserID: 2, Email: "bob@example.com", Amount: 10},
		},
	}, users)
}

func paymentStatsTestOrder(userID int64, email, currency string, amount float64, paidAt *time.Time) *dbent.PaymentOrder {
	return &dbent.PaymentOrder{
		UserID:           userID,
		UserEmail:        email,
		PayAmount:        amount,
		PaidAt:           paidAt,
		ProviderSnapshot: map[string]any{"currency": currency},
	}
}

func TestPaymentDashboardAnalyticsCountsCashflowCohortsAndCurrencies(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	first, second, older := start.Add(time.Hour), start.Add(25*time.Hour), start.Add(-25*time.Hour)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "one@example.test", "USD", 11, &first),
		paymentStatsTestOrder(1, "one@example.test", "CNY", 70, &second),
		paymentStatsTestOrder(2, "two@example.test", "USD", 22, &older),
		paymentStatsTestOrder(2, "two@example.test", "USD", 10, &second),
		{CreatedAt: second, Status: OrderStatusPending},
		paymentStatsTestOrder(3, "excluded@example.test", "USD", 999, &end),
	}
	for _, order := range orders {
		order.CreatedAt = first
		order.OrderType = "balance"
	}
	orders[0].Amount, orders[0].Status, orders[0].RefundAmount, orders[0].RefundAt = 10, OrderStatusPartiallyRefunded, 5, &second
	orders[1].Status, orders[1].OrderType = OrderStatusCompleted, "subscription"
	orders[2].CreatedAt, orders[2].Amount, orders[2].Status, orders[2].RefundAmount, orders[2].RefundAt = older, 20, OrderStatusRefunded, 20, &second
	orders[3].Amount, orders[3].Status, orders[3].RefundAmount, orders[3].RefundAt = 10, OrderStatusRefundFailed, 10, &second
	orders[5].CreatedAt = end
	stats := buildPaymentDashboard(orders, start, end, second)
	require.Equal(t, CurrencyAmounts{"USD": 21, "CNY": 70}, stats.TotalAmount)
	require.Equal(t, CurrencyAmounts{"USD": 27.5}, stats.RefundAmount)
	require.Equal(t, CurrencyAmounts{"USD": -6.5, "CNY": 70}, stats.NetAmount)
	require.Equal(t, CurrencyAmounts{"USD": 22}, stats.PreviousAmount)
	require.Equal(t, 1, stats.PreviousCount)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, 4, stats.OrderCount)
	require.Equal(t, 75.0, stats.PaidOrderRate)
	require.Equal(t, 2, stats.PayingUsers)
	require.Equal(t, 1, stats.RepeatPayers)
	require.Equal(t, 2, stats.RefundCount)
	require.Equal(t, 1, stats.PendingOrders)
	require.Len(t, stats.DailySeries, 2)
	require.Equal(t, CurrencyAmounts{"USD": -17.5, "CNY": 70}, stats.DailySeries[1].Net)
	require.Len(t, stats.OrderTypes, 2)
	require.Equal(t, 3, stats.HourlySeries[1].Count)
	require.Len(t, stats.HourlySeries, 24)
}

func TestPaymentDashboardCalendarRangeIncludesWholeDaysAcrossDST(t *testing.T) {
	now := time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)
	start, end, err := paymentDashboardRange(now, PaymentDashboardQuery{StartDate: "2026-03-07", EndDate: "2026-03-09", Timezone: "America/New_York"})
	require.NoError(t, err)
	require.Equal(t, 71*time.Hour, end.Sub(start))
	require.Equal(t, "2026-03-07 00:00:00 -0500 EST", start.String())
	stats := buildPaymentDashboard(nil, start, end, now)
	require.Len(t, stats.DailySeries, 3)
	require.Equal(t, "2026-03-07", stats.DailySeries[0].Date)
	require.Equal(t, "2026-03-09", stats.DailySeries[2].Date)
	_, _, err = paymentDashboardRange(now, PaymentDashboardQuery{Days: 367})
	require.Error(t, err)
	for _, query := range []PaymentDashboardQuery{
		{StartDate: "2026-02-30", EndDate: "2026-03-01"},
		{StartDate: "2026-03-01"}, {Timezone: "unknown/zone"},
		{StartDate: "2026-03-02", EndDate: "2026-03-01"},
	} {
		_, _, err := paymentDashboardRange(now, query)
		require.Error(t, err)
	}
}
