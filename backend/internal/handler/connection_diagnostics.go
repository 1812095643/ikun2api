package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type diagnosticWriter struct {
	gin.ResponseWriter
	collector     *requesttiming.Collector
	ctx           *gin.Context
	line          [256]byte
	lineSize      int
	frameTerminal string
	terminal      string
}

func (w *diagnosticWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// 每行只保留固定长度前缀，完整 SSE 帧结束后才确认完成，不留存对话正文。
func (w *diagnosticWriter) observe(p []byte) {
	if w.terminal != "" || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		return
	}
	for len(p) > 0 {
		newline := bytes.IndexByte(p, '\n')
		end := newline
		if end < 0 {
			end = len(p)
		}
		w.lineSize += copy(w.line[w.lineSize:], p[:end])
		if newline < 0 {
			return
		}
		w.observeLine(w.line[:w.lineSize])
		w.lineSize = 0
		if w.terminal != "" {
			return
		}
		p = p[newline+1:]
	}
}
func (w *diagnosticWriter) observeLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		if w.frameTerminal != "" {
			w.terminal = w.frameTerminal
			requesttiming.Output(w.ctx.Request.Context(), false, false, w.terminal)
		}
		w.frameTerminal = ""
		return
	}
	kind := ""
	if event, ok := bytes.CutPrefix(line, []byte("event:")); ok {
		kind = string(bytes.TrimSpace(event))
	}
	if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
		data = bytes.TrimSpace(data)
		if bytes.Equal(data, []byte("[DONE]")) {
			kind = "response.completed"
		} else if bytes.Contains(data, []byte(`"response.completed"`)) || bytes.Contains(data, []byte(`"response.failed"`)) || bytes.Contains(data, []byte(`"response.incomplete"`)) || bytes.Contains(data, []byte(`"message_stop"`)) || bytes.Contains(data, []byte(`"error"`)) {
			kind = gjson.GetBytes(data, "type").String()
		}
	}
	switch kind {
	case "response.completed", "message_stop":
		w.frameTerminal = "completed"
	case "response.failed", "error":
		w.frameTerminal = "failed"
	case "response.incomplete":
		w.frameTerminal = "incomplete"
	}
}
func (w *diagnosticWriter) Write(p []byte) (int, error) {
	start := time.Now()
	n, err := w.ResponseWriter.Write(p)
	w.collector.Written(start, n, err, false)
	if n > 0 {
		w.observe(p[:n])
	}
	return n, err
}
func (w *diagnosticWriter) WriteString(p string) (int, error) { return w.Write([]byte(p)) }
func (w *diagnosticWriter) Flush() {
	start := time.Now()
	w.ResponseWriter.Flush()
	w.collector.Written(start, 0, nil, true)
	if w.terminal != "" {
		requesttiming.TerminalFlushed(w.ctx.Request.Context(), w.terminal)
	}
}

func diagnosticString(raw string, limit int) string {
	raw = strings.ToValidUTF8(raw, "")
	if len(raw) > limit {
		raw = raw[:limit]
		raw = strings.ToValidUTF8(raw, "")
	}
	return raw
}

type connectionDiagnosticsRecorder interface {
	ConnectionDiagnosticsEnabled(context.Context) bool
	RecordConnectionTrace(service.ConnectionTrace)
}

func ConnectionDiagnosticsMiddleware(ops connectionDiagnosticsRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ops == nil || !ops.ConnectionDiagnosticsEnabled(c.Request.Context()) {
			c.Next()
			return
		}
		collector := requesttiming.New(time.Now(), c.Request.ContentLength)
		c.Request = c.Request.WithContext(requesttiming.With(c.Request.Context(), collector))
		c.Request.Body = collector.WrapBody(c.Request.Body)
		original := c.Writer
		writer := &diagnosticWriter{ResponseWriter: original, collector: collector, ctx: c}
		c.Writer = writer
		defer func() {
			if c.Writer == writer {
				c.Writer = original
			}
			// 准入拒绝沿用现有有界聚合，避免无效 Key 等流量逐条写库。
			if _, rejected := middleware.GetIngressRejectReason(c); rejected {
				return
			}
			var row service.ConnectionTrace
			row.RequestID, _ = c.Request.Context().Value(ctxkey.RequestID).(string)
			row.ClientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
			row.AccountID, _ = c.Request.Context().Value(ctxkey.AccountID).(int64)
			row.RequestID = diagnosticString(row.RequestID, 128)
			row.ClientRequestID = diagnosticString(row.ClientRequestID, 128)
			row.Model = diagnosticString(c.GetString("ops_model"), 256)
			row.Path = diagnosticString(c.FullPath(), 160)
			row.Transport = "http"
			if c.GetHeader("Upgrade") == "websocket" {
				row.Transport = "websocket"
			}
			key, _ := middleware.GetAPIKeyFromContext(c)
			if key == nil {
				key, _ = middleware.GetOpsFallbackAPIKey(c)
			}
			if key != nil {
				row.UserID = key.UserID
				row.APIKeyID = key.ID
				if key.GroupID != nil {
					row.GroupID = *key.GroupID
				}
			}
			row.AuthMS = getContextLatencyMs(c, service.OpsAuthLatencyMsKey)
			row.RoutingMS = getContextLatencyMs(c, service.OpsRoutingLatencyMsKey)
			row.UpstreamMS = getContextLatencyMs(c, service.OpsUpstreamLatencyMsKey)
			row.FirstTokenMS = getContextLatencyMs(c, service.OpsTimeToFirstTokenMsKey)
			state := "completed"
			if streamErr, ok := service.GetOpsStreamError(c); ok {
				state = "failed"
				row.ErrorType = diagnosticString(streamErr.ErrType, 96)
			}
			if writer.terminal == "failed" || writer.terminal == "incomplete" {
				state = writer.terminal
			}
			if c.Writer.Status() >= 400 {
				state = "failed"
			}
			if row.Transport == "websocket" && c.Writer.Status() < 400 {
				state = "websocket_closed"
				row.FirstTokenMS = nil
			}
			collector.WhenFinished(func(snapshot requesttiming.Snapshot) {
				telemetry.FromContext(c.Request.Context()).Timing(snapshot)
				row.TraceID = snapshot.TraceID
				row.StartedAt = snapshot.StartedAt
				row.Status = snapshot.Status
				row.TotalMS = snapshot.TotalMS
				row.RequestBytes = snapshot.BodyBytes
				row.ResponseBytes = snapshot.DownstreamBytes
				if snapshot.DownstreamError {
					state = "downstream_write_failed"
				} else if snapshot.Canceled {
					state = "client_disconnected"
				} else if snapshot.Events["body_read_error"] > 0 {
					state = "incomplete"
				}
				if row.Transport == "http" && state == "completed" && strings.Contains(writer.Header().Get("Content-Type"), "text/event-stream") && writer.terminal == "" {
					state = "unknown"
				}
				for _, attempt := range snapshot.Attempts {
					if v, ok := attempt.Events["first_byte"]; ok && (row.FirstByteMS == nil || v < *row.FirstByteMS) {
						value := v
						row.FirstByteMS = &value
					}
				}
				row.State = state
				row.Detail = &snapshot
				ops.RecordConnectionTrace(row)
			})
			collector.Finish(c.Writer.Status(), c.Request.Context().Err() != nil)
		}()
		c.Next()
	}
}
