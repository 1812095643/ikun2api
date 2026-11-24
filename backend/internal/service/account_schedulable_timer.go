package service

import "github.com/MACOS-DO/sub4api/internal/accounttimer"

func (s *adminServiceImpl) SchedulableTimers() accounttimer.Repository {
	if provider, ok := s.accountRepo.(interface {
		SchedulableTimers() accounttimer.Repository
	}); ok {
		return provider.SchedulableTimers()
	}
	return nil
}
