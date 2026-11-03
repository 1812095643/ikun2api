package handler

import (
	"net/http"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

// Handled upstream 429s belong to account availability, not request errors.
// Keep other attempts and any local failure visible; a status code alone is
// never sufficient to hide a row, and failed state writes have no receipt.
func filterOpsHandledAccountLimits(c *gin.Context, entry *service.OpsInsertErrorLogInput, snapshots ...map[int64]bool) bool {
	if c == nil || c.Request == nil || entry == nil || entry.ErrorPhase != "upstream" ||
		entry.ErrorSource != "upstream_http" || service.IsOpsRequestScopedError(c) {
		return false
	}
	ctx := c.Request.Context()
	handled := func(id int64) bool { return service.OpsAccountLimitHandled(ctx, id) }
	if len(snapshots) > 0 {
		handled = func(id int64) bool { return snapshots[0][id] }
	}
	eligibleResult := entry.StatusCode < http.StatusBadRequest || entry.StatusCode == http.StatusTooManyRequests
	if len(entry.UpstreamErrors) == 0 {
		return eligibleResult && entry.UpstreamStatusCode != nil && *entry.UpstreamStatusCode == http.StatusTooManyRequests &&
			entry.AccountID != nil && handled(*entry.AccountID)
	}
	visible := make([]*service.OpsUpstreamErrorEvent, 0, len(entry.UpstreamErrors))
	removed := 0
	for _, event := range entry.UpstreamErrors {
		if event != nil && event.UpstreamStatusCode == http.StatusTooManyRequests &&
			handled(event.AccountID) {
			removed++
			continue
		}
		visible = append(visible, event)
	}
	if removed == 0 {
		return false
	}
	if len(visible) == 0 {
		return eligibleResult
	}
	applyOpsUpstreamErrorEvents(entry, visible)
	// Do not keep the final cooled-down account as the owner of another
	// account's surviving 400/401/403/5xx diagnostic.
	for i := len(visible) - 1; i >= 0; i-- {
		if visible[i] != nil && visible[i].AccountID > 0 {
			id := visible[i].AccountID
			entry.AccountID = &id
			break
		}
	}
	return false
}
