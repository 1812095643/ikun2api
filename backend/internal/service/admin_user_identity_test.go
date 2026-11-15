//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type identityOnlyUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *identityOnlyUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.user.ID != id {
		return nil, ErrUserNotFound
	}
	return r.user, nil
}

func TestAdminService_GetUserIdentitySkipsUsageAndRates(t *testing.T) {
	user := &User{ID: 8, Username: "fixture", Email: "user@example.test"}
	repo := &identityOnlyUserRepo{user: user}
	svc := &adminServiceImpl{userRepo: repo}
	got, err := svc.GetUserIdentity(context.Background(), 8)
	require.NoError(t, err)
	require.Same(t, user, got)

	repo.err = errors.New("database unavailable")
	_, err = svc.GetUserIdentity(context.Background(), 8)
	require.ErrorIs(t, err, repo.err)
	// Any accidental enrichment calls the nil embedded repository and fails.
}
