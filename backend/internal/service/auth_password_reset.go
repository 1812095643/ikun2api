package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
)

// PasswordResetUserRepository updates only the credential, and only while the
// account still matches the snapshot to which the email link was issued.
type PasswordResetUserRepository interface {
	ResetPasswordIfUnchanged(context.Context, int64, string, string, string) (bool, error)
}

type passwordResetRequestCache interface {
	ReservePasswordResetEmail(context.Context, string) (bool, error)
}

func (s *AuthService) IsPasswordResetEnabled(ctx context.Context) bool {
	return s.settingService != nil && s.settingService.IsPasswordResetEnabled(ctx)
}

func passwordResetCredentialFingerprint(user *User) string {
	if user == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(user.Email)) + "\n" + user.PasswordHash))
	return hex.EncodeToString(sum[:])
}

// The destination comes exclusively from administrator configuration. Never
// infer a reset-link host from Host, Origin, Referer, or forwarded headers.
func passwordResetDestination(frontendURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(frontendURL))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", infraerrors.ServiceUnavailable("PASSWORD_RESET_NOT_CONFIGURED", "password reset is not configured")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", infraerrors.ServiceUnavailable("PASSWORD_RESET_NOT_CONFIGURED", "password reset requires an HTTPS site URL")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/reset-password"
	u.RawPath = ""
	return u.String(), nil
}

func (s *AuthService) requestPasswordReset(ctx context.Context, email, frontendURL, locale string, async bool) error {
	if !s.IsPasswordResetEnabled(ctx) {
		return infraerrors.Forbidden("PASSWORD_RESET_DISABLED", "password reset is not enabled")
	}
	if s.emailService == nil || s.emailService.cache == nil || (async && s.emailQueueService == nil) {
		return ErrServiceUnavailable
	}
	destination, err := passwordResetDestination(frontendURL)
	if err != nil {
		return err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 || email == "" {
		return nil
	}
	limiter, ok := s.emailService.cache.(passwordResetRequestCache)
	if !ok {
		return ErrServiceUnavailable
	}
	reserved, err := limiter.ReservePasswordResetEmail(ctx, email)
	if err != nil {
		return ErrServiceUnavailable
	}
	if !reserved {
		return nil
	}

	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			logger.LegacyPrintf("service.auth", "[Auth] Password reset account lookup failed (recipient_hash=%s)", notificationEmailHash(email))
		}
		return nil
	}
	if user == nil || !user.IsActive() || isReservedEmail(user.Email) {
		return nil
	}
	siteName := s.settingService.GetSiteName(ctx)
	binding := PasswordResetTokenData{UserID: user.ID, CredentialFingerprint: passwordResetCredentialFingerprint(user)}
	if async {
		err = s.emailQueueService.enqueueBoundPasswordReset(user.Email, siteName, destination, binding, locale)
	} else {
		err = s.emailService.sendPasswordResetEmail(ctx, user.Email, siteName, destination, &binding, locale)
	}
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Password reset delivery unavailable (recipient_hash=%s)", notificationEmailHash(email))
	}
	// The same result covers unknown, inactive, throttled, and registered emails.
	return nil
}

func (s *AuthService) RequestPasswordReset(ctx context.Context, email, frontendURL string, locale ...string) error {
	return s.requestPasswordReset(ctx, email, frontendURL, firstEmailLocale(locale), false)
}

func (s *AuthService) RequestPasswordResetAsync(ctx context.Context, email, frontendURL string, locale ...string) error {
	return s.requestPasswordReset(ctx, email, frontendURL, firstEmailLocale(locale), true)
}

func (s *AuthService) ResetPassword(ctx context.Context, email, token, newPassword string) error {
	if !s.IsPasswordResetEnabled(ctx) {
		return infraerrors.Forbidden("PASSWORD_RESET_DISABLED", "password reset is not enabled")
	}
	if s.emailService == nil || s.emailService.cache == nil {
		return ErrServiceUnavailable
	}
	// bcrypt accepts at most 72 bytes; validate before consuming a good link.
	if !utf8.ValidString(newPassword) || utf8.RuneCountInString(newPassword) < 6 || len(newPassword) > 72 {
		return infraerrors.BadRequest("INVALID_PASSWORD", "password must contain at least 6 characters and at most 72 UTF-8 bytes")
	}
	if len(token) != 64 {
		return ErrInvalidResetToken
	}
	if _, err := hex.DecodeString(token); err != nil {
		return ErrInvalidResetToken
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.emailService.VerifyPasswordResetToken(ctx, email, token); err != nil {
		return ErrInvalidResetToken
	}
	data, err := s.emailService.cache.GetPasswordResetToken(ctx, email)
	if err != nil || data == nil || data.UserID <= 0 || data.CreatedAt.IsZero() ||
		time.Since(data.CreatedAt) >= passwordResetTokenTTL || data.CreatedAt.After(time.Now().Add(time.Minute)) {
		return ErrInvalidResetToken
	}
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil || user == nil || !user.IsActive() || isReservedEmail(user.Email) || user.ID != data.UserID ||
		subtle.ConstantTimeCompare([]byte(data.CredentialFingerprint), []byte(passwordResetCredentialFingerprint(user))) != 1 {
		return ErrInvalidResetToken
	}
	writer, ok := s.userRepo.(PasswordResetUserRepository)
	if !ok {
		return ErrServiceUnavailable
	}
	previousHash := user.PasswordHash
	hashedPassword, err := s.HashPassword(newPassword)
	if err != nil {
		return ErrServiceUnavailable
	}
	if err := s.emailService.ConsumePasswordResetToken(ctx, email, token); err != nil {
		return ErrInvalidResetToken
	}
	updated, err := writer.ResetPasswordIfUnchanged(ctx, user.ID, user.Email, previousHash, hashedPassword)
	if err != nil {
		return ErrServiceUnavailable
	}
	if !updated {
		return ErrInvalidResetToken
	}
	// Existing JWT and refresh token versions derive from the password hash.
	// Revocation is additional cleanup; version mismatch remains authoritative.
	if err := s.RevokeAllUserSessions(ctx, user.ID); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Refresh session cleanup failed after password reset (user_id=%d)", user.ID)
	}
	logger.LegacyPrintf("service.auth", "[Auth] Password reset completed (user_id=%d)", user.ID)
	return nil
}
