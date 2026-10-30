package handler

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func readClaudeTelemetryBody(c *gin.Context) ([]byte, error) {
	var reader io.Reader = c.Request.Body
	encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding")))
	if encoding == "gzip" {
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer compressed.Close()
		reader = compressed
	} else if encoding != "" && encoding != "identity" {
		return nil, io.ErrUnexpectedEOF
	}
	body, err := io.ReadAll(io.LimitReader(reader, service.MaxClaudeTelemetryBodyBytes+1))
	if len(body) > service.MaxClaudeTelemetryBodyBytes {
		return nil, &http.MaxBytesError{Limit: service.MaxClaudeTelemetryBodyBytes}
	}
	return body, err
}

// ClaudeTelemetryBatch 拦截客户端遥测，不记录正文、异常文本或原始请求头。
func (h *GatewayHandler) ClaudeTelemetryBatch(c *gin.Context) {
	body, err := readClaudeTelemetryBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "遥测载荷超出限制或编码不受支持"})
		return
	}
	batch, err := service.ParseClaudeTelemetryBatch(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "遥测格式不受支持"})
		return
	}
	queued := 0
	if key, ok := middleware.GetAPIKeyFromContext(c); ok && key != nil && h != nil && h.gatewayService != nil {
		queued = h.gatewayService.RelayClaudeTelemetry(key.ID, batch)
	}
	// accepted_count=0 明确表示原始客户端事件未被转发；2xx 使客户端结束重试。
	c.JSON(http.StatusOK, gin.H{"accepted_count": 0, "filtered_count": batch.Count, "queued_count": queued})
}

// DiscardClaudeTelemetry 丢弃指标、日志、追踪及未来未知事件，不尝试解码或转发其内容。
func (h *GatewayHandler) DiscardClaudeTelemetry(c *gin.Context) {
	if _, err := io.Copy(io.Discard, io.LimitReader(c.Request.Body, service.MaxClaudeTelemetryBodyBytes+1)); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	c.JSON(http.StatusOK, gin.H{"accepted_count": 0})
}
