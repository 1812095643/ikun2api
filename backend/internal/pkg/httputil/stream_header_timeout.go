package httputil

import (
	"context"
	"errors"
)

// 响应头超时必须区别于客户端取消，才能进入现有的上游故障转移。
var ErrStreamResponseHeaderTimeout = errors.New("upstream stream response header timeout")

type streamRequestKey struct{}

// MarkStreamRequest 标记客户端流式请求，不读取或改写请求正文。
func MarkStreamRequest(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, streamRequestKey{}, true)
}

func IsStreamRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	stream, _ := ctx.Value(streamRequestKey{}).(bool)
	return stream
}
