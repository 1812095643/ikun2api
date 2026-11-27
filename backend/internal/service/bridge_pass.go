package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

var ErrBridgeRequired = errors.New(BridgeRequiredMessage)

const (
	BridgePassHeader      = "X-Ikun-Bridge-Pass"
	BridgePassTTL         = 5 * time.Minute
	BridgeRequiredCode    = "bridge_required"
	BridgeExpiredCode     = "bridge_expired"
	BridgeRequiredMessage = "This group requires IKUN Bridge. Start Bridge and try again."
	bridgePayloadSize     = 17
)

type BridgePassState uint8

const (
	BridgePassMissing BridgePassState = iota
	BridgePassValid
	BridgePassExpired
)

// The site key is separated from login tokens. The credential digest is bound
// into the MAC, so rotating a key also invalidates its outstanding passes.
type BridgePassSigner struct{ siteKey [sha256.Size]byte }

func NewBridgePassSigner(siteSecret string) *BridgePassSigner {
	if siteSecret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(siteSecret))
	mac.Write([]byte("ikun2api:bridge-pass:v1"))
	var signer BridgePassSigner
	mac.Sum(signer.siteKey[:0])
	return &signer
}

func (s *BridgePassSigner) Issue(keyID int64, key string, now time.Time) (string, time.Time) {
	if s == nil || keyID <= 0 || key == "" {
		return "", time.Time{}
	}
	expires := now.Truncate(time.Second).Add(BridgePassTTL)
	var raw [bridgePayloadSize + sha256.Size]byte
	raw[0] = 1
	binary.BigEndian.PutUint64(raw[1:9], uint64(keyID))
	binary.BigEndian.PutUint64(raw[9:17], uint64(expires.Unix()))
	s.signature(raw[:bridgePayloadSize], key, raw[bridgePayloadSize:bridgePayloadSize])
	return base64.RawURLEncoding.EncodeToString(raw[:]), expires
}

func (s *BridgePassSigner) Verify(ticket string, keyID int64, key string, now time.Time) BridgePassState {
	var raw [bridgePayloadSize + sha256.Size]byte
	if s == nil || keyID <= 0 || key == "" || len(ticket) != base64.RawURLEncoding.EncodedLen(len(raw)) {
		return BridgePassMissing
	}
	n, err := base64.RawURLEncoding.Strict().Decode(raw[:], []byte(ticket))
	if err != nil || n != len(raw) || raw[0] != 1 || binary.BigEndian.Uint64(raw[1:9]) != uint64(keyID) {
		return BridgePassMissing
	}
	var signature [sha256.Size]byte
	s.signature(raw[:bridgePayloadSize], key, signature[:0])
	if !hmac.Equal(signature[:], raw[bridgePayloadSize:]) {
		return BridgePassMissing
	}
	if int64(binary.BigEndian.Uint64(raw[9:17])) <= now.Unix() {
		return BridgePassExpired
	}
	return BridgePassValid
}

func (s *BridgePassSigner) signature(payload []byte, key string, out []byte) {
	credential := sha256.Sum256([]byte(key))
	mac := hmac.New(sha256.New, s.siteKey[:])
	mac.Write(payload)
	mac.Write(credential[:])
	mac.Sum(out)
}

type bridgePassContextKey struct{}

func WithBridgePassState(ctx context.Context, state BridgePassState) context.Context {
	return context.WithValue(ctx, bridgePassContextKey{}, state)
}

func HasValidBridgePass(ctx context.Context) bool {
	state, _ := ctx.Value(bridgePassContextKey{}).(BridgePassState)
	return state == BridgePassValid
}

func BridgeGroupDenied(ctx context.Context, group *Group) (code string) {
	if group == nil || !group.BridgeRequired {
		return ""
	}
	state, _ := ctx.Value(bridgePassContextKey{}).(BridgePassState)
	switch state {
	case BridgePassValid:
		return ""
	case BridgePassExpired:
		return BridgeExpiredCode
	default:
		return BridgeRequiredCode
	}
}
