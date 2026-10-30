//go:build unit

package service

import (
	"context"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type passwordResetTestCache struct {
	EmailCache
	mu         sync.Mutex
	data       *PasswordResetTokenData
	reserved   bool
	reserveErr error
}

func (c *passwordResetTestCache) ReservePasswordResetEmail(context.Context, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reserveErr != nil {
		return false, c.reserveErr
	}
	if c.reserved {
		return false, nil
	}
	c.reserved = true
	return true, nil
}
func (c *passwordResetTestCache) GetPasswordResetToken(context.Context, string) (*PasswordResetTokenData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		return nil, nil
	}
	copy := *c.data
	return &copy, nil
}
func (c *passwordResetTestCache) SetPasswordResetToken(_ context.Context, _ string, d *PasswordResetTokenData, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := *d
	c.data = &copy
	return nil
}
func (c *passwordResetTestCache) ConsumePasswordResetToken(_ context.Context, _ string, hash string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil || c.data.Token != hash {
		return false, nil
	}
	c.data = nil
	return true, nil
}

type passwordResetTestRepo struct {
	UserRepository
	mu        sync.Mutex
	user      User
	rejectCAS bool
	writes    int
}

func (r *passwordResetTestRepo) GetByEmail(_ context.Context, email string) (*User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strings.EqualFold(email, r.user.Email) {
		return nil, ErrUserNotFound
	}
	copy := r.user
	return &copy, nil
}
func (r *passwordResetTestRepo) ResetPasswordIfUnchanged(_ context.Context, id int64, email, old, next string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rejectCAS || r.user.ID != id || r.user.Email != email || r.user.PasswordHash != old || !r.user.IsActive() {
		return false, nil
	}
	r.user.PasswordHash = next
	r.writes++
	return true, nil
}

type passwordResetTestRefresh struct {
	RefreshTokenCache
	deleted atomic.Int32
}

func (r *passwordResetTestRefresh) DeleteUserRefreshTokens(context.Context, int64) error {
	r.deleted.Add(1)
	return nil
}

func newPasswordResetTestService(t *testing.T) (*AuthService, *passwordResetTestCache, *passwordResetTestRepo, *passwordResetTestRefresh) {
	t.Helper()
	cache := &passwordResetTestCache{}
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	require.NoError(t, err)
	repo := &passwordResetTestRepo{user: User{ID: 42, Email: "user@example.com", PasswordHash: string(hash), Status: StatusActive, Role: RoleUser, Balance: 15}}
	refresh := &passwordResetTestRefresh{}
	settings := newNotificationEmailMemorySettingRepo()
	require.NoError(t, settings.SetMultiple(context.Background(), map[string]string{SettingKeyPasswordResetEnabled: "true", SettingKeyEmailVerifyEnabled: "false"}))
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "reset-test-secret", ExpireHour: 1}}
	svc := &AuthService{userRepo: repo, refreshTokenCache: refresh, cfg: cfg, settingService: NewSettingService(settings, cfg), emailService: NewEmailService(settings, cache), emailQueueService: &EmailQueueService{taskChan: make(chan EmailTask, 10)}}
	return svc, cache, repo, refresh
}
func seedPasswordReset(t *testing.T, svc *AuthService, c *passwordResetTestCache, r *passwordResetTestRepo) string {
	t.Helper()
	token, err := svc.emailService.GeneratePasswordResetToken()
	require.NoError(t, err)
	require.NoError(t, c.SetPasswordResetToken(context.Background(), r.user.Email, &PasswordResetTokenData{Token: hashPasswordResetToken(token), CreatedAt: time.Now(), UserID: r.user.ID, CredentialFingerprint: passwordResetCredentialFingerprint(&r.user)}, passwordResetTokenTTL))
	return token
}
func TestPasswordResetFlowEmailAndSingleUse(t *testing.T) {
	svc, cache, repo, refresh := newPasswordResetTestService(t)
	ctx := context.Background()
	smtp := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, svc.emailService.settingRepo.SetMultiple(ctx, smtp.settings()))
	oldAccess, err := svc.GenerateToken(ctx, &repo.user)
	require.NoError(t, err)
	require.NoError(t, svc.RequestPasswordResetAsync(ctx, " USER@EXAMPLE.COM ", "https://example.com", "zh-CN"))
	task := <-svc.emailQueueService.taskChan
	require.NotNil(t, task.ResetBinding)
	require.Equal(t, int64(42), task.ResetBinding.UserID)
	svc.emailQueueService.emailService = svc.emailService
	svc.emailQueueService.processTask(0, task)
	require.Equal(t, int64(1), smtp.messageCount())
	link := regexp.MustCompile(`href="([^"]*reset-password[^"]*)"`).FindStringSubmatch(smtp.lastMessageBody(t))
	require.Len(t, link, 2)
	u, err := url.Parse(html.UnescapeString(link[1]))
	require.NoError(t, err)
	require.Empty(t, u.RawQuery)
	fragment, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	token := fragment.Get("token")
	require.Len(t, token, 64)
	require.NotEqual(t, token, cache.data.Token)
	require.NoError(t, svc.ResetPassword(ctx, "user@example.com", token, "new-password"))
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(repo.user.PasswordHash), []byte("new-password")))
	require.Equal(t, 15.0, repo.user.Balance)
	require.Equal(t, int32(1), refresh.deleted.Load())
	claims, err := svc.ValidateToken(oldAccess)
	require.NoError(t, err)
	require.NotEqual(t, claims.TokenVersion, resolvedTokenVersion(&repo.user))
	require.ErrorIs(t, svc.ResetPassword(ctx, "user@example.com", token, "another-password"), ErrInvalidResetToken)
	require.Equal(t, 1, repo.writes)
}
func TestPasswordResetRejectsStaleOrInvalidLinks(t *testing.T) {
	for _, kind := range []string{"changed-password", "recreated-user", "expired", "wrong-token", "wrong-email", "inactive", "unbound", "cas-race"} {
		t.Run(kind, func(t *testing.T) {
			svc, cache, repo, _ := newPasswordResetTestService(t)
			token := seedPasswordReset(t, svc, cache, repo)
			email := "user@example.com"
			switch kind {
			case "changed-password":
				repo.user.PasswordHash = "newer-hash"
			case "recreated-user":
				repo.user.ID++
			case "expired":
				cache.data.CreatedAt = time.Now().Add(-31 * time.Minute)
			case "wrong-token":
				token = strings.Repeat("0", 64)
			case "wrong-email":
				email = "other@example.com"
			case "inactive":
				repo.user.Status = "disabled"
			case "unbound":
				cache.data.UserID = 0
			case "cas-race":
				repo.rejectCAS = true
			}
			require.ErrorIs(t, svc.ResetPassword(context.Background(), email, token, "new-password"), ErrInvalidResetToken)
			require.Zero(t, repo.writes)
		})
	}
}
func TestPasswordResetBadPasswordDoesNotConsumeLink(t *testing.T) {
	svc, c, r, _ := newPasswordResetTestService(t)
	token := seedPasswordReset(t, svc, c, r)
	for _, password := range []string{"short", strings.Repeat("a", 73), strings.Repeat("中", 25)} {
		require.Error(t, svc.ResetPassword(context.Background(), r.user.Email, token, password))
		require.NotNil(t, c.data)
	}
	require.NoError(t, svc.ResetPassword(context.Background(), r.user.Email, token, "new-password"))
}
func TestPasswordResetConcurrentSubmission(t *testing.T) {
	svc, c, r, _ := newPasswordResetTestService(t)
	token := seedPasswordReset(t, svc, c, r)
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.ResetPassword(context.Background(), "user@example.com", token, "new-password") == nil {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), won.Load())
	require.Equal(t, 1, r.writes)
}
func TestPasswordResetRequestPrivacyAndCacheFailure(t *testing.T) {
	for _, email := range []string{"user@example.com", "unknown@example.com"} {
		svc, c, _, _ := newPasswordResetTestService(t)
		require.NoError(t, svc.RequestPasswordResetAsync(context.Background(), email, "https://example.com"))
		require.NoError(t, svc.RequestPasswordResetAsync(context.Background(), email, "https://example.com"))
		if email == "unknown@example.com" {
			require.Empty(t, svc.emailQueueService.taskChan)
		} else {
			require.Len(t, svc.emailQueueService.taskChan, 1)
		}
		c.reserveErr = errors.New("redis unavailable")
		require.ErrorIs(t, svc.RequestPasswordResetAsync(context.Background(), email, "https://example.com"), ErrServiceUnavailable)
	}
}
func TestPasswordResetDestinationTrustBoundary(t *testing.T) {
	for _, base := range []string{"", "https://user:pass@example.com", "http://example.com", "//example.com", "javascript:alert(1)", "https://example.com?next=evil", "https://example.com/#fragment"} {
		_, err := passwordResetDestination(base)
		require.Error(t, err, base)
	}
	got, err := passwordResetDestination("https://example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/reset-password", got)
}
func TestPasswordResetIndependentOfRegistrationVerification(t *testing.T) {
	svc, _, _, _ := newPasswordResetTestService(t)
	require.True(t, svc.IsPasswordResetEnabled(context.Background()))
	public, err := svc.settingService.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, public.PasswordResetEnabled)
	require.False(t, public.EmailVerifyEnabled)
	parsed := svc.settingService.parseSettings(map[string]string{SettingKeyPasswordResetEnabled: "true", SettingKeyEmailVerifyEnabled: "false"})
	require.True(t, parsed.PasswordResetEnabled)
}
