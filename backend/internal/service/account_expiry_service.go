package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/accounttimer"
)

// AccountExpiryService periodically pauses expired accounts when auto-pause is enabled.
type AccountExpiryService struct {
	accountRepo AccountRepository
	interval    time.Duration
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewAccountExpiryService(accountRepo AccountRepository, interval time.Duration) *AccountExpiryService {
	return &AccountExpiryService{
		accountRepo: accountRepo,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
}

func (s *AccountExpiryService) Start() {
	if s == nil || s.accountRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		timers := time.NewTicker(accounttimer.PollInterval)
		defer timers.Stop()

		s.runOnce()
		s.runTimers()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-timers.C:
				s.runTimers()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// 定时启停只修改调度开关，账号健康检查、冷却和并发限制仍独立生效。
func (s *AccountExpiryService) runTimers() {
	provider, ok := s.accountRepo.(interface {
		SchedulableTimers() accounttimer.Repository
	})
	if !ok || provider.SchedulableTimers() == nil {
		return
	}
	repo := provider.SchedulableTimers()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := repo.RunDue(ctx); err != nil {
		log.Printf("[AccountTimer] execution failed: %v", err)
	}
	if recurring, ok := repo.(interface{ RunRecurringDue(context.Context) error }); ok {
		if err := recurring.RunRecurringDue(ctx); err != nil {
			log.Printf("[AccountTimer] recurring execution failed: %v", err)
		}
	}
}

func (s *AccountExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AccountExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updated, err := s.accountRepo.AutoPauseExpiredAccounts(ctx, time.Now())
	if err != nil {
		log.Printf("[AccountExpiry] Auto pause expired accounts failed: %v", err)
		return
	}
	if updated > 0 {
		log.Printf("[AccountExpiry] Auto paused %d expired accounts", updated)
	}
}
