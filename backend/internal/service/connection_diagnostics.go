package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/google/uuid"
)

const ConnectionTraceRetentionDays = 3

type ConnectionTrace struct {
	TraceID         string                  `json:"trace_id"`
	RequestID       string                  `json:"request_id"`
	ClientRequestID string                  `json:"client_request_id"`
	StartedAt       time.Time               `json:"started_at"`
	UserID          int64                   `json:"user_id"`
	APIKeyID        int64                   `json:"api_key_id"`
	AccountID       int64                   `json:"account_id"`
	GroupID         int64                   `json:"group_id"`
	Model           string                  `json:"model"`
	Path            string                  `json:"path"`
	Transport       string                  `json:"transport"`
	State           string                  `json:"state"`
	Status          int                     `json:"status"`
	TotalMS         float64                 `json:"total_ms"`
	FirstByteMS     *float64                `json:"first_byte_ms"`
	AuthMS          *int64                  `json:"auth_ms"`
	RoutingMS       *int64                  `json:"routing_ms"`
	UpstreamMS      *int64                  `json:"upstream_ms"`
	FirstTokenMS    *int64                  `json:"first_token_ms"`
	RequestBytes    int64                   `json:"request_bytes"`
	ResponseBytes   int64                   `json:"response_bytes"`
	ErrorType       string                  `json:"error_type"`
	DetailAvailable bool                    `json:"detail_available"`
	Detail          *requesttiming.Snapshot `json:"detail,omitempty"`
}

type ConnectionTraceFilter struct {
	Start, End                           time.Time
	UserID, APIKeyID, AccountID, GroupID int64
	Model, RequestID, State, Transport   string
	BeforeTime                           time.Time
	BeforeID                             string
	Limit                                int
}

type ConnectionTracePage struct {
	Items         []ConnectionTrace     `json:"items"`
	NextCursor    string                `json:"next_cursor"`
	RetentionDays int                   `json:"retention_days"`
	Health        ConnectionTraceHealth `json:"health"`
}

type ConnectionTraceHealth struct {
	Queued            int    `json:"queued"`
	Written           uint64 `json:"written"`
	Dropped           uint64 `json:"dropped"`
	WriteFailed       uint64 `json:"write_failed"`
	MaintenanceFailed uint64 `json:"maintenance_failed"`
}

type ConnectionTraceRepository interface {
	InsertConnectionTraces(context.Context, []ConnectionTrace) error
	ListConnectionTraces(context.Context, ConnectionTraceFilter) ([]ConnectionTrace, error)
	GetConnectionTrace(context.Context, time.Time, string) (*ConnectionTrace, error)
	MaintainConnectionTracePartitions(context.Context, time.Time) error
}

type connectionTraceSink struct {
	repo                                        ConnectionTraceRepository
	queue                                       chan ConnectionTrace
	stop                                        chan struct{}
	done                                        chan struct{}
	once                                        sync.Once
	written, dropped, failed, maintenanceFailed atomic.Uint64
	mu                                          sync.RWMutex
	closed                                      bool
}

func newConnectionTraceSink(repo ConnectionTraceRepository) *connectionTraceSink {
	s := &connectionTraceSink{repo: repo, queue: make(chan ConnectionTrace, 2048), stop: make(chan struct{}), done: make(chan struct{})}
	go s.run()
	return s
}

func (s *connectionTraceSink) health() ConnectionTraceHealth {
	if s == nil {
		return ConnectionTraceHealth{}
	}
	return ConnectionTraceHealth{Queued: len(s.queue), Written: s.written.Load(), Dropped: s.dropped.Load(), WriteFailed: s.failed.Load(), MaintenanceFailed: s.maintenanceFailed.Load()}
}

func (s *OpsService) ConnectionDiagnosticsEnabled(ctx context.Context) bool {
	return s != nil && s.connectionTraces != nil && s.IsMonitoringEnabled(ctx)
}

// 正常请求只保存摘要；错误、首字慢请求和少量成功样本保留分段详情。
func (s *OpsService) RecordConnectionTrace(record ConnectionTrace) {
	sink := s.connectionTraces
	if sink == nil {
		return
	}
	if record.Detail != nil {
		id, err := uuid.Parse(record.TraceID)
		sample := err == nil && id[0] < 3
		if record.State == "completed" && (record.FirstTokenMS == nil || *record.FirstTokenMS < 5000) && (record.FirstByteMS == nil || *record.FirstByteMS < 5000) && record.TotalMS < 60000 && !sample {
			record.Detail = nil
		}
	}
	record.DetailAvailable = record.Detail != nil
	sink.mu.RLock()
	defer sink.mu.RUnlock()
	if sink.closed {
		sink.dropped.Add(1)
		return
	}
	// 有界队列溢出时只丢观测数据，不给模型转发增加排队和数据库等待。
	select {
	case sink.queue <- record:
	default:
		sink.dropped.Add(1)
	}
}

func (s *OpsService) StopConnectionDiagnostics() {
	if s == nil || s.connectionTraces == nil {
		return
	}
	sink := s.connectionTraces
	sink.once.Do(func() { sink.mu.Lock(); sink.closed = true; close(sink.stop); sink.mu.Unlock() })
	<-sink.done
}

func (s *connectionTraceSink) run() {
	defer close(s.done)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	maintenance := time.NewTicker(time.Hour)
	defer maintenance.Stop()
	maintain := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.MaintainConnectionTracePartitions(ctx, time.Now().UTC()); err != nil {
			s.maintenanceFailed.Add(1)
		}
	}
	maintain()
	batch := make([]ConnectionTrace, 0, 100)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := s.repo.InsertConnectionTraces(ctx, batch)
		cancel()
		if err != nil {
			s.failed.Add(uint64(len(batch)))
		} else {
			s.written.Add(uint64(len(batch)))
		}
		clear(batch)
		batch = batch[:0]
	}
	for {
		select {
		case record := <-s.queue:
			batch = append(batch, record)
			if len(batch) == cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-maintenance.C:
			maintain()
		case <-s.stop:
			deadline := time.Now().Add(3 * time.Second)
			for {
				select {
				case record := <-s.queue:
					if time.Now().After(deadline) {
						s.dropped.Add(1)
						continue
					}
					batch = append(batch, record)
					if len(batch) == cap(batch) {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (s *OpsService) ListConnectionTraces(ctx context.Context, filter ConnectionTraceFilter) (*ConnectionTracePage, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.connectionTraces == nil {
		return nil, infraerrors.NotFound("DIAGNOSTICS_UNAVAILABLE", "连接诊断暂不可用")
	}
	if filter.End.IsZero() {
		filter.End = time.Now()
	}
	if filter.Start.IsZero() {
		filter.Start = filter.End.Add(-time.Hour)
	}
	if !filter.Start.Before(filter.End) || filter.End.Sub(filter.Start) > 7*24*time.Hour {
		return nil, infraerrors.BadRequest("INVALID_TIME_RANGE", "请选择七天以内的有效时间范围")
	}
	if filter.Limit < 1 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	items, err := s.connectionTraces.repo.ListConnectionTraces(ctx, filter)
	if err != nil {
		return nil, err
	}
	next := ""
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		last := items[len(items)-1]
		next = EncodeConnectionCursor(last.StartedAt, last.TraceID)
	}
	if items == nil {
		items = []ConnectionTrace{}
	}
	return &ConnectionTracePage{Items: items, NextCursor: next, RetentionDays: ConnectionTraceRetentionDays, Health: s.connectionTraces.health()}, nil
}

func (s *OpsService) GetConnectionTrace(ctx context.Context, at time.Time, id string) (*ConnectionTrace, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.connectionTraces == nil {
		return nil, infraerrors.NotFound("DIAGNOSTICS_UNAVAILABLE", "连接诊断暂不可用")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.connectionTraces.repo.GetConnectionTrace(ctx, at, id)
}

func EncodeConnectionCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}
func DecodeConnectionCursor(raw string) (time.Time, string, error) {
	if len(raw) > 160 {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", err
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", err
	}
	if _, err = uuid.Parse(id); err != nil {
		return time.Time{}, "", err
	}
	return t, id, nil
}

func EncodeConnectionDetail(detail *requesttiming.Snapshot) (any, error) {
	if detail == nil {
		return nil, nil
	}
	raw, err := json.Marshal(detail)
	if len(raw) > 48*1024 {
		return nil, nil
	}
	return string(raw), err
}
