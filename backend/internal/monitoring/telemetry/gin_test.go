package telemetry

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkHTTPObservation(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			if enabled {
				r.Use(New("test", "i", "b", 4096, 8192).Middleware())
			}
			r.POST("/chat", func(c *gin.Context) {
				c.Header("Content-Type", "text/event-stream")
				c.Writer.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
			})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/chat", nil))
			}
		})
	}
}

func TestHTTP200StreamFailureAndBodyReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := New("test", "i", "b", 10, 100)
	r := gin.New()
	r.Use(h.Middleware())
	r.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(200)
		c.Writer.WriteString("event: error\ndata: {\"error\":{\"message\":\"private\"}}\n\n")
	})
	r.POST("/reject", func(c *gin.Context) { c.AbortWithStatusJSON(413, gin.H{"error": gin.H{"code": "body_limit"}}) })
	for _, path := range []string{"/stream", "/reject"} {
		method := "GET"
		if path == "/reject" {
			method = "POST"
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		s := h.Snapshot(0, time.Now())
		last := s.Events[len(s.Events)-1]
		if last.Outcome != "failed" {
			t.Fatalf("lost failure %+v", last)
		}
		if path == "/stream" && last.ClientStatus != 200 {
			t.Fatal("wire status changed")
		}
	}
}
func TestNormalDailyLimitRemainsClassified(t *testing.T) {
	h := New("test", "i", "b", 10, 100)
	r := gin.New()
	r.Use(h.Middleware())
	r.GET("/limit", func(c *gin.Context) {
		FromContext(c.Request.Context()).LocalError("key_daily_limit")
		c.JSON(429, gin.H{"error": gin.H{"code": "API_KEY_RATE_1D_EXCEEDED"}})
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/limit", nil))
	s := h.Snapshot(0, time.Now())
	last := s.Events[len(s.Events)-1]
	if last.Reason != "key_daily_limit" {
		t.Fatal(last)
	}
}
func TestUpstreamCannotSpoofNormalDailyLimit(t *testing.T) {
	h := New("test", "i", "b", 10, 100)
	r := gin.New()
	r.Use(h.Middleware())
	r.POST("/v1/chat/completions", func(c *gin.Context) { c.JSON(429, gin.H{"error": gin.H{"code": "API_KEY_RATE_1D_EXCEEDED"}}) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", nil))
	events := h.Snapshot(0, time.Now()).Events
	if events[len(events)-1].Reason == "key_daily_limit" {
		t.Fatal("upstream spoofed trusted local quota")
	}
}
func TestExpectedChatProtocolCannotSilentlyBecomeUnknown(t *testing.T) {
	h := New("test", "i", "b", 10, 100)
	r := gin.New()
	r.Use(h.Middleware())
	r.POST("/v1/chat/completions", func(c *gin.Context) { c.JSON(200, gin.H{"unexpected": "value"}) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", nil))
	events := h.Snapshot(0, time.Now()).Events
	last := events[len(events)-1]
	if last.Reason != "wrong_protocol" || last.Outcome != "failed" {
		t.Fatal(last)
	}
}
