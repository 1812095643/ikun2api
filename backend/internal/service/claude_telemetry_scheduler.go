package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
)

// ClaudeTelemetryScheduler 只定时领取真实请求摘要，与客户端触发共享队列和发送出口。
type ClaudeTelemetryScheduler struct {
	gateway        *GatewayService
	keyRepo        APIKeyRepository
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	wake           chan struct{}
	mu             sync.Mutex
	stopped        bool
	inflight       sync.WaitGroup
	deliveryCtx    context.Context
	deliveryCancel context.CancelFunc
	settings       ClaudeTelemetrySettings
	configured     bool
	generation     uint64
}

func NewClaudeTelemetryScheduler(gateway *GatewayService, keyRepo APIKeyRepository) *ClaudeTelemetryScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &ClaudeTelemetryScheduler{gateway: gateway, keyRepo: keyRepo, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	if gateway != nil {
		gateway.claudeTelemetryScheduler = s
	}
	if gateway == nil || gateway.httpUpstream == nil || keyRepo == nil {
		close(s.done)
		return s
	}
	if settings := gateway.settingService; settings != nil {
		settings.claudeTelemetrySettingsMu.Lock()
		settings.claudeTelemetrySettingsChanged = func(_ ClaudeTelemetrySettings) { s.applySettings() }
		settings.claudeTelemetrySettingsMu.Unlock()
	}
	s.applySettings()
	go s.run()
	logger.LegacyPrintf("service.claude_telemetry", "scheduler=started max_concurrent=1 pending_batches=%d", cap(claudeTelemetryPendingSlots))
	return s
}

// Settings changes cancel queued/in-flight deliveries and discard old summaries.
// Re-enabling starts from new observations instead of replaying old activity.
func (s *ClaudeTelemetryScheduler) applySettings() {
	s.mu.Lock()
	settings := s.gateway.telemetrySettings()
	if s.stopped || (s.configured && s.settings == settings) {
		s.mu.Unlock()
		return
	}
	if s.deliveryCancel != nil {
		s.deliveryCancel()
	}
	if s.configured && s.gateway != nil {
		s.gateway.claudeTelemetry.clear()
	}
	s.deliveryCtx, s.deliveryCancel = context.WithCancel(s.ctx)
	s.settings, s.configured = settings, true
	s.generation++
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ClaudeTelemetryScheduler) observationGeneration() uint64 {
	s.applySettings()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}

func (s *ClaudeTelemetryScheduler) remember(apiKeyID int64, session string, delivery claudeTelemetryDelivery, generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.settings.Enabled || generation != s.generation || !s.gateway.telemetrySettings().Enabled {
		return
	}
	delivery.generation = generation
	s.gateway.claudeTelemetry.remember(apiKeyID, session, delivery, time.Now(), time.Duration(s.settings.FlushIntervalSeconds)*time.Second)
}

func (s *ClaudeTelemetryScheduler) run() {
	defer close(s.done)
	// Refresh configuration changed by another instance, even with an empty queue.
	configTicker := time.NewTicker(5 * time.Second)
	defer configTicker.Stop()
	var timer *time.Timer
	var ticks <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	reschedule := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		ticks = nil
		if !s.gateway.telemetrySettings().Enabled {
			s.gateway.claudeTelemetry.clear()
			return
		}
		delay, pending := s.gateway.claudeTelemetry.nextDelay(time.Now())
		if !pending {
			return
		}
		// 槽位占满时按同一周期稍后再检查，避免到期摘要导致空转。
		if delay == 0 && len(claudeTelemetryPendingSlots) == cap(claudeTelemetryPendingSlots) {
			delay = claudeTelemetryInterval
		}
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		ticks = timer.C
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			reschedule()
		case <-configTicker.C:
			s.applySettings()
			reschedule()
		case <-ticks:
			s.flush(time.Now())
			reschedule()
		}
	}
}

func (s *ClaudeTelemetryScheduler) flush(now time.Time) int {
	if s.ctx.Err() != nil || s.gateway == nil {
		return 0
	}
	if !s.gateway.telemetrySettings().Enabled {
		s.gateway.claudeTelemetry.clear()
		return 0
	}
	available := cap(claudeTelemetryPendingSlots) - len(claudeTelemetryPendingSlots)
	queued := 0
	for _, delivery := range s.gateway.claudeTelemetry.claimDue(now, available) {
		if s.gateway.dispatchClaudeTelemetry(delivery) {
			queued++
		}
	}
	return queued
}

func (s *ClaudeTelemetryScheduler) beginDelivery(generations ...uint64) (context.Context, bool) {
	s.applySettings()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.ctx.Err() != nil || !s.settings.Enabled {
		return nil, false
	}
	if len(generations) > 0 && generations[0] != 0 && generations[0] != s.generation {
		return nil, false
	}
	s.inflight.Add(1)
	return s.deliveryCtx, true
}

// 等待期间 Key、用户或分组可能已被停用；只复核身份，不重复计费或更新使用时间。
func (s *ClaudeTelemetryScheduler) validateOrigin(ctx context.Context, delivery claudeTelemetryDelivery) error {
	if s.keyRepo == nil || delivery.apiKeyID <= 0 || delivery.userID <= 0 {
		return fmt.Errorf("遥测缺少原请求身份")
	}
	key, err := s.keyRepo.GetByID(ctx, delivery.apiKeyID)
	if err != nil || key == nil || key.ID != delivery.apiKeyID || key.UserID != delivery.userID || key.User == nil || key.User.ID != delivery.userID || key.User.DeletedAt != nil || !key.User.IsActive() {
		return fmt.Errorf("遥测原请求身份已不可用")
	}
	if !key.IsActive() && key.Status != StatusAPIKeyExpired && key.Status != StatusAPIKeyQuotaExhausted {
		return fmt.Errorf("遥测原请求 Key 已停用")
	}
	groupID := int64(0)
	if key.GroupID != nil {
		groupID = *key.GroupID
		if key.Group == nil || !key.Group.IsActive() {
			return fmt.Errorf("遥测原请求分组已不可用")
		}
		if !key.Group.IsSubscriptionType() && !key.User.CanBindGroup(groupID, key.Group.IsExclusive) {
			return fmt.Errorf("遥测原请求分组权限已撤销")
		}
	}
	if groupID != delivery.groupID {
		return fmt.Errorf("遥测原请求分组已变更")
	}
	return nil
}

func (s *ClaudeTelemetryScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	s.cancel()
	s.mu.Unlock()
	<-s.done
	s.inflight.Wait()
	if s.gateway != nil {
		s.gateway.claudeTelemetry.clear()
	}
}
