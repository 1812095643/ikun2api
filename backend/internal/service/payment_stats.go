package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"time"

	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/ent/paymentauditlog"
	"github.com/MACOS-DO/sub4api/ent/paymentorder"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
)

// --- Dashboard & Analytics ---

func (s *PaymentService) GetDashboardStats(ctx context.Context, days int) (*DashboardStats, error) {
	return s.GetDashboardAnalytics(ctx, PaymentDashboardQuery{Days: days})
}

type PaymentDashboardQuery struct {
	Days      int
	StartDate string
	EndDate   string
	Timezone  string
}

func paymentDashboardRange(now time.Time, query PaymentDashboardQuery) (time.Time, time.Time, error) {
	location := time.Local
	if query.Timezone != "" {
		var err error
		location, err = time.LoadLocation(query.Timezone)
		if err != nil {
			return time.Time{}, time.Time{}, infraerrors.BadRequest("INVALID_TIMEZONE", "请选择有效时区")
		}
	}
	now = now.In(location)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
	days := query.Days
	if days <= 0 {
		days = 30
	}
	start := end.AddDate(0, 0, -days)
	if query.StartDate != "" || query.EndDate != "" {
		var err error
		start, err = time.ParseInLocation("2006-01-02", query.StartDate, location)
		if err != nil {
			return time.Time{}, time.Time{}, infraerrors.BadRequest("INVALID_DATE_RANGE", "请选择完整的日期范围")
		}
		end, err = time.ParseInLocation("2006-01-02", query.EndDate, location)
		if err != nil {
			return time.Time{}, time.Time{}, infraerrors.BadRequest("INVALID_DATE_RANGE", "请选择完整的日期范围")
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.Before(end) || end.After(start.AddDate(0, 0, 366)) {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("INVALID_DATE_RANGE", "请选择一年以内的日期范围")
	}
	return start, end, nil
}

func (s *PaymentService) GetDashboardAnalytics(ctx context.Context, query PaymentDashboardQuery) (*DashboardStats, error) {
	start, end, err := paymentDashboardRange(time.Now(), query)
	if err != nil {
		return nil, err
	}
	previousStart := start.AddDate(0, 0, -paymentDashboardDays(start, end))
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.Or(
		paymentorder.And(paymentorder.PaidAtGTE(previousStart), paymentorder.PaidAtLT(end)),
		paymentorder.And(paymentorder.CreatedAtGTE(start), paymentorder.CreatedAtLT(end)),
		paymentorder.And(paymentorder.RefundAtGTE(start), paymentorder.RefundAtLT(end)),
	)).Select(paymentorder.FieldUserID, paymentorder.FieldUserEmail, paymentorder.FieldAmount,
		paymentorder.FieldPayAmount, paymentorder.FieldPaymentType, paymentorder.FieldOrderType,
		paymentorder.FieldStatus, paymentorder.FieldPaidAt, paymentorder.FieldRefundAt,
		paymentorder.FieldRefundAmount, paymentorder.FieldCreatedAt, paymentorder.FieldProviderSnapshot).All(ctx)
	if err != nil {
		return nil, err
	}
	return buildPaymentDashboard(orders, start, end, time.Now()), nil
}

func paymentDashboardDays(start, end time.Time) int {
	days := 0
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		days++
	}
	return days
}

func buildPaymentDashboard(orders []*dbent.PaymentOrder, start, end, now time.Time) *DashboardStats {
	paid := []*dbent.PaymentOrder{}
	previous := []*dbent.PaymentOrder{}
	users := map[int64]int{}
	statuses := map[string]int{}
	st := &DashboardStats{RefundAmount: CurrencyAmounts{}, NetAmount: CurrencyAmounts{}, HourlySeries: make([]PaymentHourStat, 24)}
	refundByDay := map[string]CurrencyAmounts{}
	paidCreated := 0
	previousStart := start.AddDate(0, 0, -paymentDashboardDays(start, end))
	// 收款和退款使用各自的发生时间；支付率使用创建订单的队列，避免混用统计分母。
	for _, order := range orders {
		if !order.CreatedAt.Before(start) && order.CreatedAt.Before(end) {
			st.OrderCount++
			statuses[order.Status]++
			if order.PaidAt != nil {
				paidCreated++
			}
			if order.Status == OrderStatusPending {
				st.PendingOrders++
			}
		}
		if order.PaidAt != nil && !order.PaidAt.Before(start) && order.PaidAt.Before(end) {
			paid = append(paid, order)
			users[order.UserID]++
			hour := order.PaidAt.In(start.Location()).Hour()
			st.HourlySeries[hour].Count++
		} else if order.PaidAt != nil && !order.PaidAt.Before(previousStart) && order.PaidAt.Before(start) {
			previous = append(previous, order)
		}
		// refund_amount 是余额单位，复用退款模块换算网关实退款；仅计入已经完成的退款。
		if order.RefundAt != nil && !order.RefundAt.Before(start) && order.RefundAt.Before(end) &&
			(order.Status == OrderStatusRefunded || order.Status == OrderStatusPartiallyRefunded) {
			currency := PaymentOrderCurrency(order)
			amount := calculateGatewayRefundAmount(order.Amount, order.PayAmount, order.RefundAmount, currency)
			st.RefundAmount[currency] += amount
			st.RefundCount++
			day := order.RefundAt.In(start.Location()).Format("2006-01-02")
			if refundByDay[day] == nil {
				refundByDay[day] = CurrencyAmounts{}
			}
			refundByDay[day][currency] += amount
		}
	}
	localNow := now.In(start.Location())
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, start.Location())
	computeBasicStats(st, paid, today)
	previousStats := &DashboardStats{}
	computeBasicStats(previousStats, previous, today)
	st.PreviousAmount, st.PreviousCount = previousStats.TotalAmount, previousStats.TotalCount
	st.PayingUsers = len(users)
	for _, count := range users {
		if count > 1 {
			st.RepeatPayers++
		}
	}
	if st.OrderCount > 0 {
		st.PaidOrderRate = roundAmount(float64(paidCreated) * 100 / float64(st.OrderCount))
	}
	st.OrderStatuses = []PaymentOrderStatusStat{}
	for status, count := range statuses {
		st.OrderStatuses = append(st.OrderStatuses, PaymentOrderStatusStat{Status: status, Count: count})
	}
	sort.Slice(st.OrderStatuses, func(i, j int) bool { return st.OrderStatuses[i].Status < st.OrderStatuses[j].Status })
	for hour := range st.HourlySeries {
		st.HourlySeries[hour].Hour = hour
	}
	for currency, amount := range st.TotalAmount {
		st.NetAmount[currency] = amount
	}
	for currency, amount := range st.RefundAmount {
		st.NetAmount[currency] -= amount
	}
	roundCurrencyAmounts(st.NetAmount)
	roundCurrencyAmounts(st.RefundAmount)
	days := paymentDashboardDays(start, end)
	st.DailySeries = buildDailySeries(paid, start.AddDate(0, 0, -1), days)
	for i := range st.DailySeries {
		day := &st.DailySeries[i]
		day.Refund = refundByDay[day.Date]
		day.Net = CurrencyAmounts{}
		for currency, amount := range day.Amount {
			day.Net[currency] = amount
		}
		for currency, amount := range day.Refund {
			day.Net[currency] -= amount
		}
		roundCurrencyAmounts(day.Refund)
		roundCurrencyAmounts(day.Net)
	}
	st.PaymentMethods = buildMethodDistribution(paid)
	st.OrderTypes = buildPaymentDistribution(paid, func(order *dbent.PaymentOrder) string {
		if order.OrderType == "" {
			return "balance"
		}
		return order.OrderType
	})
	st.TopUsers = buildTopUsers(paid)
	return st
}

func computeBasicStats(st *DashboardStats, orders []*dbent.PaymentOrder, todayStart time.Time) {
	st.TotalAmount = make(CurrencyAmounts)
	st.TodayAmount = make(CurrencyAmounts)
	st.AvgAmount = make(CurrencyAmounts)
	currencyCounts := make(map[string]int)
	var todayCount int
	for _, o := range orders {
		currency := PaymentOrderCurrency(o)
		st.TotalAmount[currency] += o.PayAmount
		currencyCounts[currency]++
		if o.PaidAt != nil && !o.PaidAt.Before(todayStart) {
			st.TodayAmount[currency] += o.PayAmount
			todayCount++
		}
	}
	st.TotalCount = len(orders)
	st.TodayCount = todayCount
	for currency, totalAmount := range st.TotalAmount {
		st.AvgAmount[currency] = roundAmount(totalAmount / float64(currencyCounts[currency]))
	}
	roundCurrencyAmounts(st.TotalAmount)
	roundCurrencyAmounts(st.TodayAmount)
}

func buildDailySeries(orders []*dbent.PaymentOrder, since time.Time, days int) []DailyStats {
	dailyMap := make(map[string]*DailyStats)
	for _, o := range orders {
		if o.PaidAt == nil {
			continue
		}
		date := o.PaidAt.In(since.Location()).Format("2006-01-02")
		ds, ok := dailyMap[date]
		if !ok {
			ds = &DailyStats{Date: date, Amount: make(CurrencyAmounts)}
			dailyMap[date] = ds
		}
		ds.Amount[PaymentOrderCurrency(o)] += o.PayAmount
		ds.Count++
	}
	series := make([]DailyStats, 0, days)
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i+1).Format("2006-01-02")
		if ds, ok := dailyMap[date]; ok {
			roundCurrencyAmounts(ds.Amount)
			series = append(series, *ds)
		} else {
			series = append(series, DailyStats{Date: date, Amount: make(CurrencyAmounts)})
		}
	}
	return series
}

func buildMethodDistribution(orders []*dbent.PaymentOrder) []PaymentMethodStat {
	return buildPaymentDistribution(orders, func(order *dbent.PaymentOrder) string { return order.PaymentType })
}

func buildPaymentDistribution(orders []*dbent.PaymentOrder, key func(*dbent.PaymentOrder) string) []PaymentMethodStat {
	methodMap := make(map[string]*PaymentMethodStat)
	for _, o := range orders {
		typ := key(o)
		ms, ok := methodMap[typ]
		if !ok {
			ms = &PaymentMethodStat{Type: typ, Amount: make(CurrencyAmounts)}
			methodMap[typ] = ms
		}
		ms.Amount[PaymentOrderCurrency(o)] += o.PayAmount
		ms.Count++
	}
	methods := make([]PaymentMethodStat, 0, len(methodMap))
	for _, ms := range methodMap {
		roundCurrencyAmounts(ms.Amount)
		methods = append(methods, *ms)
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Type < methods[j].Type
	})
	return methods
}

func buildTopUsers(orders []*dbent.PaymentOrder) TopUsersByCurrency {
	userMap := make(map[string]map[int64]*TopUserStat)
	for _, o := range orders {
		currency := PaymentOrderCurrency(o)
		users, ok := userMap[currency]
		if !ok {
			users = make(map[int64]*TopUserStat)
			userMap[currency] = users
		}
		us, ok := users[o.UserID]
		if !ok {
			us = &TopUserStat{UserID: o.UserID, Email: o.UserEmail}
			users[o.UserID] = us
		}
		us.Amount += o.PayAmount
	}
	result := make(TopUsersByCurrency, len(userMap))
	for currency, users := range userMap {
		userList := make([]*TopUserStat, 0, len(users))
		for _, us := range users {
			us.Amount = roundAmount(us.Amount)
			userList = append(userList, us)
		}
		sort.Slice(userList, func(i, j int) bool {
			return userList[i].Amount > userList[j].Amount
		})
		limit := topUsersLimit
		if len(userList) < limit {
			limit = len(userList)
		}
		result[currency] = make([]TopUserStat, 0, limit)
		for i := 0; i < limit; i++ {
			result[currency] = append(result[currency], *userList[i])
		}
	}
	return result
}

func roundCurrencyAmounts(amounts CurrencyAmounts) {
	for currency, amount := range amounts {
		amounts[currency] = roundAmount(amount)
	}
}

func roundAmount(amount float64) float64 {
	return math.Round(amount*100) / 100
}

// --- Audit Logs ---

func (s *PaymentService) writeAuditLog(ctx context.Context, oid int64, action, op string, detail map[string]any) {
	dj, _ := json.Marshal(detail)
	_, err := s.entClient.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(oid, 10)).SetAction(action).SetDetail(string(dj)).SetOperator(op).Save(ctx)
	if err != nil {
		slog.Error("audit log failed", "orderID", oid, "action", action, "error", err)
	}
}

func (s *PaymentService) GetOrderAuditLogs(ctx context.Context, oid int64) ([]*dbent.PaymentAuditLog, error) {
	return s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(oid, 10))).Order(paymentauditlog.ByCreatedAt()).All(ctx)
}
