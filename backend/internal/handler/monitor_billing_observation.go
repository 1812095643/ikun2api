package handler

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	pkgerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

// Preserve the trusted local error identity before protocol adapters collapse it
// into a generic rate_limit_exceeded code. Never derive this exemption from an upstream body.
func billingErrorDetailsObserved(c *gin.Context, err error) (int, string, string, int) {
	if c != nil && c.Request != nil {
		telemetry.FromContext(c.Request.Context()).LocalError(telemetry.LocalReasonCode(pkgerrors.Reason(err)))
	}
	return billingErrorDetails(err)
}
