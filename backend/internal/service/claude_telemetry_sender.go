package service

import (
	"context"
	"time"
)

// 这是网关限流策略，不是官方客户端的发送间隔。
const claudeTelemetryMinSendGap = 2 * time.Second

// 整个进程共用一个发送槽，避免不同账号及两种触发方式同时向上游发请求。
var claudeTelemetrySender = claudeTelemetrySerialSender{slot: make(chan struct{}, 1)}

type claudeTelemetrySerialSender struct {
	slot         chan struct{}
	nextAt       time.Time
	lastFinished time.Time
}

func (s *claudeTelemetrySerialSender) acquire(ctx context.Context, gaps ...time.Duration) (func(), error) {
	gap := claudeTelemetryMinSendGap
	if len(gaps) > 0 && gaps[0] >= claudeTelemetryMinSendGap {
		gap = gaps[0]
	}
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	nextAt := s.nextAt
	if updated := s.lastFinished.Add(gap); updated.After(nextAt) {
		nextAt = updated
	}
	if delay := time.Until(nextAt); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			<-s.slot
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		<-s.slot
		return nil, err
	}
	return func() {
		// 从上一批结束开始留出间隔；失败也不能立即密集重发。
		s.lastFinished = time.Now()
		s.nextAt = s.lastFinished.Add(gap)
		<-s.slot
	}, nil
}
