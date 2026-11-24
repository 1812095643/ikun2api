//go:build integration

package repository

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

type lockedReductionReadRepo struct {
	service.UserSubscriptionRepository
	afterRead func(*service.UserSubscription)
	lockReads atomic.Int32
}

func (r *lockedReductionReadRepo) GetByUserIDAndGroupID(ctx context.Context, u, g int64) (*service.UserSubscription, error) {
	sub, err := r.UserSubscriptionRepository.GetByUserIDAndGroupID(ctx, u, g)
	if err == nil && r.afterRead != nil {
		r.afterRead(sub)
	}
	return sub, err
}
func (r *lockedReductionReadRepo) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	r.lockReads.Add(1)
	return r.UserSubscriptionRepository.GetByIDForUpdate(ctx, id)
}
func newReductionLockFixture(t *testing.T, name string) (*service.RedeemService, *lockedReductionReadRepo, *service.UserSubscription, int64, []string) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	user, err := client.User.Create().SetEmail(name + "@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName(name).Save(ctx)
	require.NoError(t, err)
	// Redemption commits its own transaction; remove fixtures explicitly so
	// repository list/count tests cannot observe this test's subscriptions.
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM redeem_codes WHERE group_id=$1`, group.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM user_subscriptions WHERE user_id=$1`, user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id=$1`, group.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
		require.NoError(t, err)
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	sub, err := client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.AddDate(0, 0, 10)).SetStatus(service.SubscriptionStatusActive).SetNotes("initial").Save(ctx)
	require.NoError(t, err)
	repo := &lockedReductionReadRepo{UserSubscriptionRepository: NewUserSubscriptionRepository(client)}
	codes := NewRedeemCodeRepository(client)
	values := []string{name + "-first", name + "-second"}
	for _, value := range values {
		require.NoError(t, codes.Create(ctx, &service.RedeemCode{Code: value, Type: service.RedeemTypeSubscription, Status: service.StatusUnused, ValidityDays: -1, GroupID: &group.ID}))
	}
	subscriptions := service.NewSubscriptionService(nil, repo, nil, client, nil)
	redeem := service.NewRedeemService(codes, NewUserRepository(client, integrationDB), subscriptions, nil, nil, client, nil, nil)
	return redeem, repo, userSubscriptionEntityToService(sub), user.ID, values
}

// Legacy group subscriptions remain readable, but redemption is retired.
// Exercise rejection against PostgreSQL instead of reviving the removed path.
func TestLegacyRedeemReductionPreservesRenewal(t *testing.T) {
	redeem, repo, sub, userID, codes := newReductionLockFixture(t, "reduction-renewal")
	ctx := context.Background()
	renewed := sub.ExpiresAt.AddDate(0, 0, 10)
	require.NoError(t, repo.UserSubscriptionRepository.ExtendExpiry(ctx, sub.ID, renewed))
	require.NoError(t, repo.UserSubscriptionRepository.UpdateNotes(ctx, sub.ID, "renewal"))
	_, err := redeem.RedeemForAdminFulfillment(ctx, userID, codes[0])
	require.ErrorIs(t, err, service.ErrLegacySubscriptionRetired)
	got, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, renewed.Equal(got.ExpiresAt))
	require.Equal(t, "renewal", got.Notes)
	require.Zero(t, repo.lockReads.Load())
	code, err := NewRedeemCodeRepository(testEntClient(t)).GetByCode(ctx, codes[0])
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, code.Status)
}

func TestLegacyRedeemConcurrentReductionsBothRejected(t *testing.T) {
	redeem, repo, sub, userID, codes := newReductionLockFixture(t, "reduction-concurrent")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, code := range codes {
		go func(code string) {
			<-start
			_, err := redeem.RedeemForAdminFulfillment(ctx, userID, code)
			results <- err
		}(code)
	}
	close(start)
	for range 2 {
		select {
		case err := <-results:
			require.ErrorIs(t, err, service.ErrLegacySubscriptionRetired)
		case <-ctx.Done():
			t.Fatal("retired redemption did not finish")
		}
	}
	got, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, sub.ExpiresAt.Equal(got.ExpiresAt))
	require.Equal(t, sub.Notes, got.Notes)
	for _, value := range codes {
		code, err := NewRedeemCodeRepository(testEntClient(t)).GetByCode(ctx, value)
		require.NoError(t, err)
		require.Equal(t, service.StatusUnused, code.Status)
	}
	require.Zero(t, repo.lockReads.Load())
}
