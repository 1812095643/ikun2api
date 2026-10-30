package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	dbuser "github.com/MACOS-DO/sub4api/ent/user"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/redis/go-redis/v9"
)

var _ service.PasswordResetUserRepository = (*userRepository)(nil)

func (r *userRepository) ResetPasswordIfUnchanged(ctx context.Context, userID int64, email, previousHash, nextHash string) (bool, error) {
	n, err := clientFromContext(ctx, r.client).User.Update().Where(
		dbuser.IDEQ(userID), dbuser.EmailEQ(email), dbuser.PasswordHashEQ(previousHash),
		dbuser.StatusEQ(service.StatusActive), dbuser.DeletedAtIsNil(),
	).SetPasswordHash(nextHash).Save(ctx)
	return n == 1, err
}

// The request is reserved before looking up the email, so queue congestion,
// cooldowns, and hourly limits do not reveal whether an account exists.
var reservePasswordResetEmailScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 5 then return 0 end
if tonumber(redis.call('GET', KEYS[3]) or '0') >= 10 then return 0 end
redis.call('SET', KEYS[1], '1', 'EX', 60)
local h = redis.call('INCR', KEYS[2])
if h == 1 then redis.call('EXPIRE', KEYS[2], 3600) end
local d = redis.call('INCR', KEYS[3])
if d == 1 then redis.call('EXPIRE', KEYS[3], 86400) end
return 1
`)

func (c *emailCache) ReservePasswordResetEmail(ctx context.Context, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	digest := sha256.Sum256([]byte(email))
	key := "password_reset_rate:" + hex.EncodeToString(digest[:])
	n, err := reservePasswordResetEmailScript.Run(ctx, c.rdb, []string{
		passwordResetSentAtKey(email), key + ":hour", key + ":day",
	}).Int()
	return n == 1, err
}
