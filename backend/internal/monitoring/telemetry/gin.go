package telemetry

import (
	"bufio"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var installed atomic.Pointer[Hub]

func Default() *Hub { return installed.Load() }

func Install(h *Hub) { installed.Store(h) }

func (h *Hub) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h == nil || h.Enabled != nil && !h.Enabled() || FromContext(c.Request.Context()) != nil {
			c.Next()
			return
		}
		id, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		if id == "" {
			id = uuid.NewString()
		}
		ctx, t := h.Start(c.Request.Context(), id, time.Now())
		c.Request = c.Request.WithContext(ctx)
		path := c.Request.URL.Path
		for _, suffix := range []string{"/chat/completions", "/messages", "/responses", ":generateContent", ":streamGenerateContent"} {
			if strings.HasSuffix(path, suffix) {
				t.Update(domain.Observation{Metrics: map[string]float64{"expected_text_protocol": 1}})
				break
			}
		}
		writer := &captureWriter{ResponseWriter: c.Writer, tracker: t}
		writer.parser = NewParser(func(s Signal) { t.Signal(s, time.Now()) })
		c.Writer = writer
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Signal(Signal{Failed: true, Reason: "handler_panic"}, time.Now())
				t.Finish(500, false, time.Now())
				panic(recovered)
			}
			writer.finish()
			code := c.GetString("ops_local_error_code")
			if reason := LocalReasonCode(code); reason != "unclassified" {
				t.Update(domain.Observation{Reason: reason})
			}
			t.Finish(c.Writer.Status(), c.Request.Context().Err() != nil, time.Now())
		}()
		c.Next()
	}
}

type captureWriter struct {
	gin.ResponseWriter
	tracker            *Tracker
	parser             *Parser
	body               []byte
	overflow, hijacked bool
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *captureWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if n > 0 && !w.hijacked {
		if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
			w.parser.Feed(b[:n])
		} else if strings.Contains(w.Header().Get("Content-Type"), "json") {
			if len(w.body)+n <= maxFrame*4 {
				w.body = append(w.body, b[:n]...)
			} else {
				w.overflow = true
				w.body = nil
			}
		}
	}
	if err != nil {
		w.tracker.Signal(Signal{Failed: true, Reason: "client_write_failed"}, time.Now())
	}
	return n, err
}
func (w *captureWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	w.tracker.Update(domain.Observation{Metrics: map[string]float64{"realtime": 1}})
	return w.ResponseWriter.Hijack()
}
func (w *captureWriter) finish() {
	if w.overflow || w.hijacked {
		w.tracker.Signal(Signal{Partial: true}, time.Now())
		return
	}
	if len(w.body) > 0 {
		w.parser.JSON(w.body)
		clear(w.body)
		w.body = nil
	}
}
func LocalReasonCode(s string) string {
	switch s {
	case "API_KEY_RATE_1D_EXCEEDED":
		return "key_daily_limit"
	case "API_KEY_RATE_5H_EXCEEDED", "API_KEY_RATE_7D_EXCEEDED":
		return "key_window_limit"
	case "INSUFFICIENT_BALANCE", "INSUFFICIENT_QUOTA", "SUBSCRIPTION_QUOTA_EXCEEDED", "API_KEY_QUOTA_EXHAUSTED", "UNIVERSAL_SUBSCRIPTION_EXHAUSTED", "UNIVERSAL_SUBSCRIPTION_NOT_ACTIVE", "UNIVERSAL_DAILY_LIMIT", "UNIVERSAL_WEEKLY_LIMIT", "UNIVERSAL_MONTHLY_LIMIT":
		return "user_balance_exhausted"
	case "gateway_queue_full", "gateway_slot_timeout":
		return "queue_timeout"
	case "insufficient_quota", "insufficient_balance", "quota_exhausted":
		return "upstream_quota_exhausted"
	default:
		return "unclassified"
	}
}

func ReasonCode(s string) string {
	switch s {
	case "insufficient_quota", "insufficient_balance", "quota_exhausted":
		return "upstream_quota_exhausted"
	default:
		return "unclassified"
	}
}
