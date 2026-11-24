package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	pkghttputil "github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/MACOS-DO/sub4api/internal/service"
)

func (s *httpUpstreamService) streamResponseHeaderTimeout(profile service.HTTPUpstreamProfile) time.Duration {
	// OpenAI、Grok 及显式长连接通道继续使用各自的超时设置。
	if s.cfg == nil || profile != service.HTTPUpstreamProfileDefault || s.cfg.Gateway.StreamResponseHeaderTimeout <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.StreamResponseHeaderTimeout) * time.Second
}

func withStreamResponseHeaderTimeout(req *http.Request, budget time.Duration) (*http.Request, func() bool) {
	if budget <= 0 || !pkghttputil.IsStreamRequest(req.Context()) {
		return req, func() bool { return false }
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	var mu sync.Mutex
	var timer *time.Timer
	finished := false
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if finished || timer != nil {
				return
			}
			// 上传完成后才计时，避免大正文和建连耗时侵占等待响应头的预算。
			timer = time.AfterFunc(budget, func() {
				mu.Lock()
				defer mu.Unlock()
				if !finished {
					cancel(pkghttputil.ErrStreamResponseHeaderTimeout)
				}
			})
		},
	}
	finish := func() bool {
		mu.Lock()
		defer mu.Unlock()
		finished = true
		if timer != nil {
			timer.Stop()
		}
		// 先发生的客户端取消保留原原因；收到响应头后禁止定时器取消响应正文。
		return errors.Is(context.Cause(ctx), pkghttputil.ErrStreamResponseHeaderTimeout)
	}
	return req.WithContext(httptrace.WithClientTrace(ctx, trace)), finish
}
