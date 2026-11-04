package handler

import (
	"github.com/MACOS-DO/sub4api/internal/handler/dto"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"

	"github.com/gin-gonic/gin"
)

// SubscriptionSummaryItem represents a subscription item in summary
type SubscriptionSummaryItem struct {
	ID              int64   `json:"id"`
	GroupID         int64   `json:"group_id"`
	GroupName       string  `json:"group_name"`
	Status          string  `json:"status"`
	DailyUsedUSD    float64 `json:"daily_used_usd,omitempty"`
	DailyLimitUSD   float64 `json:"daily_limit_usd,omitempty"`
	WeeklyUsedUSD   float64 `json:"weekly_used_usd,omitempty"`
	WeeklyLimitUSD  float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyUsedUSD  float64 `json:"monthly_used_usd,omitempty"`
	MonthlyLimitUSD float64 `json:"monthly_limit_usd,omitempty"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
}

// SubscriptionProgressInfo represents subscription with progress info
type SubscriptionProgressInfo struct {
	Subscription *dto.UserSubscription         `json:"subscription"`
	Progress     *service.SubscriptionProgress `json:"progress"`
}

// SubscriptionHandler handles user subscription operations
type SubscriptionHandler struct {
	subscriptionService *service.SubscriptionService
}

// NewSubscriptionHandler creates a new user subscription handler
func NewSubscriptionHandler(subscriptionService *service.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
	}
}

// List handles listing current user's subscriptions
// GET /api/v1/subscriptions
func (h *SubscriptionHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	subscriptions, err := h.subscriptionService.ListUserSubscriptions(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.UserSubscription, 0, len(subscriptions))
	for i := range subscriptions {
		out = append(out, *dto.UserSubscriptionFromService(&subscriptions[i]).Localized(c.GetHeader("Accept-Language")))
	}
	response.Success(c, out)
}

// GetActive handles getting current user's active subscriptions
// GET /api/v1/subscriptions/active
func (h *SubscriptionHandler) GetActive(c *gin.Context) {
	c.Header("Deprecation", "true")
	c.Header("Link", "</api/v1/subscriptions/universal>; rel=\"successor-version\"")
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	response.Success(c, []any{})
}

// GetProgress handles getting subscription progress for current user
// GET /api/v1/subscriptions/progress
func (h *SubscriptionHandler) GetProgress(c *gin.Context) {
	c.Header("Deprecation", "true")
	c.Header("Link", "</api/v1/subscriptions/universal>; rel=\"successor-version\"")
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	response.Success(c, []any{})
}

// GetSummary handles getting a summary of current user's subscription status
// GET /api/v1/subscriptions/summary
func (h *SubscriptionHandler) GetSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	all, _, err := h.subscriptionService.Universal().List(c.Request.Context(), subject.UserID, 0, 200)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := []gin.H{}
	totalUsed := 0.0
	for _, sub := range all {
		if sub.Status != "active" {
			continue
		}
		totalUsed += sub.TotalUsedUSD
		progress := func(used float64, limit *float64) any {
			if limit == nil {
				return nil
			}
			return used / *limit * 100
		}
		items = append(items, gin.H{"id": sub.ID, "group_name": sub.Name, "status": sub.Status, "daily_used_usd": sub.DailyUsedUSD, "weekly_used_usd": sub.WeeklyUsedUSD, "monthly_used_usd": sub.MonthlyUsedUSD, "daily_limit_usd": sub.DailyLimitUSD, "weekly_limit_usd": sub.WeeklyLimitUSD, "monthly_limit_usd": sub.MonthlyLimitUSD, "daily_progress": progress(sub.DailyUsedUSD, sub.DailyLimitUSD), "weekly_progress": progress(sub.WeeklyUsedUSD, sub.WeeklyLimitUSD), "monthly_progress": progress(sub.MonthlyUsedUSD, sub.MonthlyLimitUSD), "expires_at": sub.ExpiresAt, "remaining_usd": sub.RemainingUSD, "total_credit_usd": sub.TotalCreditUSD})
	}
	response.Success(c, gin.H{"active_count": len(items), "total_used_usd": totalUsed, "subscriptions": items})
}
