package httputil

import (
	"context"
	"errors"
)

// ErrStreamResponseHeaderTimeout is returned by the upstream transport when a
// streaming request got no response headers within the stream header budget.
// It deliberately does not wrap context.Canceled, so callers fail over to
// another account instead of treating it as a client disconnect.
var ErrStreamResponseHeaderTimeout = errors.New("upstream stream response header timeout")

type streamRequestKey struct{}

// MarkStreamRequest tags an upstream request context as a client-streaming
// request. A healthy upstream answers a stream with headers almost at once, so
// the transport bounds the header wait for these much tighter than for
// non-streaming requests, whose headers only arrive after the full response.
func MarkStreamRequest(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, streamRequestKey{}, true)
}

// IsStreamRequest reports whether ctx was tagged by MarkStreamRequest.
func IsStreamRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(streamRequestKey{}).(bool)
	return v
}
