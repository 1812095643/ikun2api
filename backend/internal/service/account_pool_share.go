package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
)

type poolShareRepository interface {
	SavePoolShare(context.Context, int64, string, time.Time) error
	ResolvePoolShare(context.Context, string, time.Time) (int64, error)
	RevokePoolShare(context.Context, int64) error
}
type PoolShareLink struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func poolShareHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *AccountPoolService) CreateShareLink(ctx context.Context, userID int64) (*PoolShareLink, error) {
	store, ok := s.usageLogRepo.(poolShareRepository)
	if !ok {
		return nil, poolDataUnavailable(nil)
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	token := "pool_" + base64.RawURLEncoding.EncodeToString(bytes)
	expires := time.Now().Add(30 * 24 * time.Hour)
	if err := store.SavePoolShare(ctx, userID, poolShareHash(token), expires); err != nil {
		return nil, err
	}
	return &PoolShareLink{Token: token, ExpiresAt: expires}, nil
}
func (s *AccountPoolService) ResolveShareLink(ctx context.Context, token string) (int64, error) {
	invalid := infraerrors.Unauthorized("POOL_SHARE_INVALID", "分享链接无效、已撤销或已过期")
	if len(token) != 48 || !strings.HasPrefix(token, "pool_") {
		return 0, invalid
	}
	store, ok := s.usageLogRepo.(poolShareRepository)
	if !ok {
		return 0, poolDataUnavailable(nil)
	}
	userID, err := store.ResolvePoolShare(ctx, poolShareHash(token), time.Now())
	if err != nil {
		return 0, poolDataUnavailable(err)
	}
	if userID <= 0 {
		return 0, invalid
	}
	return userID, nil
}
func (s *AccountPoolService) RevokeShareLink(ctx context.Context, userID int64) error {
	store, ok := s.usageLogRepo.(poolShareRepository)
	if !ok {
		return poolDataUnavailable(nil)
	}
	return store.RevokePoolShare(ctx, userID)
}

// Public projection deliberately has no internal IDs, group membership,
// concurrency, usage, windows or credentials. Both logged-in and shared views
// use exactly this response contract.
type PoolPublicAccount struct {
	Name     string           `json:"name"`
	Platform string           `json:"platform"`
	Status   string           `json:"status"`
	Capacity PoolUserCapacity `json:"capacity"`
	FiveHour PoolPublicWindow `json:"five_hour"`
	SevenDay PoolPublicWindow `json:"seven_day"`
}
type PoolPublicWindow struct {
	Utilization float64 `json:"utilization"`
	Enabled     bool    `json:"enabled"`
}

func PoolPublicAccounts(items []PoolUserAccount) []PoolPublicAccount {
	result := make([]PoolPublicAccount, 0, len(items))
	for _, item := range items {
		result = append(result, PoolPublicAccount{Name: item.Name, Platform: item.Platform, Status: item.Status, Capacity: item.Capacity,
			FiveHour: PoolPublicWindow{Utilization: item.FiveHour.Utilization, Enabled: item.FiveHour.Enabled}, SevenDay: PoolPublicWindow{Utilization: item.SevenDay.Utilization, Enabled: item.SevenDay.Enabled}})
	}
	return result
}
