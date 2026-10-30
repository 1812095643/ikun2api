package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPasswordResetRepositoryConditionalWrite(t *testing.T) {
	r, _ := newUserEntRepo(t)
	ctx := context.Background()
	u := &service.User{Email: "reset@example.com", PasswordHash: "original", Status: service.StatusActive, Role: service.RoleUser, Balance: 25}
	require.NoError(t, r.Create(ctx, u))
	for _, wrong := range []struct {
		id          int64
		email, hash string
	}{{u.ID + 1, u.Email, "original"}, {u.ID, "other@example.com", "original"}, {u.ID, u.Email, "stale"}} {
		ok, err := r.ResetPasswordIfUnchanged(ctx, wrong.id, wrong.email, wrong.hash, "new")
		require.NoError(t, err)
		require.False(t, ok)
	}
	ok, err := r.ResetPasswordIfUnchanged(ctx, u.ID, u.Email, "original", "new")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.ResetPasswordIfUnchanged(ctx, u.ID, u.Email, "original", "overwrite")
	require.NoError(t, err)
	require.False(t, ok)
	stored, err := r.GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, "new", stored.PasswordHash)
	require.Equal(t, 25.0, stored.Balance)
	stored.Status = "disabled"
	require.NoError(t, r.Update(ctx, stored, service.UserUpdateFields{Status: true}))
	ok, err = r.ResetPasswordIfUnchanged(ctx, u.ID, u.Email, "new", "overwrite")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestPasswordResetReservationConcurrencyAndBudgets(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	c := cache.(*emailCache)
	ctx := context.Background()
	email := "test@example.com"
	var wg sync.WaitGroup
	var won atomic.Int32
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := c.ReservePasswordResetEmail(ctx, email)
			if err == nil && ok {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), won.Load())
	for i := 0; i < 4; i++ {
		mr.FastForward(time.Minute)
		ok, err := c.ReservePasswordResetEmail(ctx, " TEST@EXAMPLE.COM ")
		require.NoError(t, err)
		require.True(t, ok)
	}
	mr.FastForward(time.Minute)
	ok, err := c.ReservePasswordResetEmail(ctx, email)
	require.NoError(t, err)
	require.False(t, ok, "hourly budget")
	mr.FastForward(time.Hour)
	for i := 0; i < 5; i++ {
		ok, err = c.ReservePasswordResetEmail(ctx, email)
		require.NoError(t, err)
		require.True(t, ok)
		mr.FastForward(time.Minute)
	}
	mr.FastForward(time.Hour)
	ok, err = c.ReservePasswordResetEmail(ctx, email)
	require.NoError(t, err)
	require.False(t, ok, "daily budget")
	mr.FastForward(24 * time.Hour)
	ok, err = c.ReservePasswordResetEmail(ctx, email)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestPasswordResetTokenExpiresInRedis(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	svc := service.NewEmailService(nil, cache)
	token, err := svc.GeneratePasswordResetToken()
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(token))
	require.NoError(t, cache.SetPasswordResetToken(ctx, "a@example.com", &service.PasswordResetTokenData{Token: hex.EncodeToString(digest[:])}, 30*time.Minute))
	require.NoError(t, svc.VerifyPasswordResetToken(ctx, "a@example.com", token))
	mr.FastForward(30 * time.Minute)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, "a@example.com", token), service.ErrInvalidResetToken)
}
