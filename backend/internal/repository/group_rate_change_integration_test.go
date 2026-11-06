//go:build integration

package repository

import (
	"github.com/MACOS-DO/sub4api/internal/service"
)

func (s *GroupRepoSuite) TestPriceChangeHistory() {
	group := &service.Group{Name: "rate-history", Platform: service.PlatformAnthropic,
		RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}
	s.Require().NoError(s.repo.Create(s.ctx, group))
	got, err := s.repo.GetByID(s.ctx, group.ID)
	s.Require().NoError(err)
	s.Nil(got.PreviousRateMultiplier)
	s.Nil(got.RateMultiplierChangedAt)
	staleSnapshot := *got

	group.RateMultiplier = 0.8
	s.Require().NoError(s.repo.Update(s.ctx, group))
	s.Require().NotNil(group.PreviousRateMultiplier)
	s.Equal(1.0, *group.PreviousRateMultiplier)
	s.Require().NotNil(group.RateMultiplierChangedAt)
	firstChange := *group.RateMultiplierChangedAt

	group.Name = "renamed-without-repricing"
	s.Require().NoError(s.repo.Update(s.ctx, group))
	s.Equal(1.0, *group.PreviousRateMultiplier)
	s.True(firstChange.Equal(*group.RateMultiplierChangedAt))

	// The baseline must come from the locked database row, not a stale caller.
	staleSnapshot.RateMultiplier = 1.3
	s.Require().NoError(s.repo.Update(s.ctx, &staleSnapshot))
	s.Equal(0.8, *staleSnapshot.PreviousRateMultiplier)
	latestChange := *staleSnapshot.RateMultiplierChangedAt

	// Values equal at the persisted DECIMAL precision are not new price changes.
	staleSnapshot.RateMultiplier = 1.30001
	s.Require().NoError(s.repo.Update(s.ctx, &staleSnapshot))
	s.Equal(0.8, *staleSnapshot.PreviousRateMultiplier)
	s.True(latestChange.Equal(*staleSnapshot.RateMultiplierChangedAt))

	_, err = s.tx.ExecContext(s.ctx, "UPDATE groups SET rate_multiplier = 0.9 WHERE id = $1", group.ID)
	s.Require().NoError(err)
	got, err = s.repo.GetByID(s.ctx, group.ID)
	s.Require().NoError(err)
	s.Equal(1.3, *got.PreviousRateMultiplier)
	s.Equal(0.9, got.RateMultiplier)

	// Duplicating a group creates a new baseline; it must not inherit changes.
	got.ID = 0
	got.Name = "copied-rate-history"
	s.Require().NoError(s.repo.Create(s.ctx, got))
	copied, err := s.repo.GetByID(s.ctx, got.ID)
	s.Require().NoError(err)
	s.Nil(copied.PreviousRateMultiplier)
	s.Nil(copied.RateMultiplierChangedAt)
}
