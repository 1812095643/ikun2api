package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Client configuration comes from the reviewed profile catalog, never inbound headers.
func defaultFingerprint() Fingerprint {
	p := claude.EffectiveClientProfile()
	return Fingerprint{
		UserAgent:     "claude-cli/" + p.CLIVersion + " (external, cli)",
		StainlessLang: "js", StainlessPackageVersion: p.SDKVersion,
		StainlessOS: p.OS, StainlessArch: p.Arch,
		StainlessRuntime: p.Runtime, StainlessRuntimeVersion: p.RuntimeVersion,
		ProfileID: p.ID, TLSSampleID: p.TLSSampleID, ProfileSource: p.Source,
	}
}

// Fingerprint represents account fingerprint data
type Fingerprint struct {
	ProfileID               string `json:",omitempty"`
	TLSSampleID             string `json:",omitempty"`
	ProfileSource           string `json:",omitempty"`
	IdentityVersion         int    `json:",omitempty"`
	ClientID                string
	UserAgent               string
	StainlessLang           string
	StainlessPackageVersion string
	StainlessOS             string
	StainlessArch           string
	StainlessRuntime        string
	StainlessRuntimeVersion string
	UpdatedAt               int64 `json:",omitempty"` // Unix timestamp，用于判断是否需要续期TTL
}

// IdentityCache defines cache operations for identity service
type IdentityCache interface {
	GetFingerprint(ctx context.Context, accountID int64) (*Fingerprint, error)
	SetFingerprint(ctx context.Context, accountID int64, fp *Fingerprint) error
	// Session mask seeds are account-scoped and expire after 15 idle minutes.
	GetSessionMaskSeed(ctx context.Context, accountID int64) (string, error)
	// An empty candidate only refreshes an existing seed; creation returns the atomic winner.
	GetOrCreateSessionMaskSeed(ctx context.Context, accountID int64, candidate string) (string, error)
	GetClaudeCodeHeaders(ctx context.Context, accountID int64) (http.Header, error)
	UpdateClaudeCodeHeaders(ctx context.Context, accountID int64, headers http.Header) error
}

// IdentityStore owns immutable account device IDs. Redis is only a cache.
type IdentityStore interface {
	GetDeviceID(ctx context.Context, accountID int64) (string, error)
	GetOrCreateDeviceID(ctx context.Context, accountID int64, candidate string) (string, error)
	MigrateLegacyFingerprints(ctx context.Context) error
}

func ValidClaudeDeviceID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type IdentityService struct {
	cache IdentityCache
	store IdentityStore
}

func NewIdentityService(cache IdentityCache, store IdentityStore) *IdentityService {
	return &IdentityService{cache: cache, store: store}
}

// Import every still-live legacy ID before accepting traffic, including inactive
// accounts whose Redis entry would otherwise expire before their next request.
func ProvideIdentityService(cache IdentityCache, store IdentityStore) (*IdentityService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.MigrateLegacyFingerprints(ctx); err != nil {
		return nil, fmt.Errorf("migrate Claude device identities: %w", err)
	}
	return NewIdentityService(cache, store), nil
}

// Ordinary request headers never write permanent client configuration.
func (s *IdentityService) GetOrCreateFingerprint(ctx context.Context, accountID int64, _ http.Header) (*Fingerprint, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("Claude identity store is unavailable")
	}
	var cached *Fingerprint
	var cacheErr error
	if s.cache != nil {
		cached, cacheErr = s.cache.GetFingerprint(ctx, accountID)
	}
	id := ""
	// This marker is only written after the SQL winner has been read. Legacy cache
	// entries must be reconciled with SQL even if their ID happens to be valid.
	if cached != nil && cached.IdentityVersion == 1 && ValidClaudeDeviceID(cached.ClientID) {
		id = cached.ClientID
	} else {
		var err error
		id, err = s.store.GetDeviceID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if id == "" {
			if cacheErr != nil {
				return nil, fmt.Errorf("cannot inherit Claude device identity: %w", cacheErr)
			}
			candidate := ""
			if cached != nil && ValidClaudeDeviceID(cached.ClientID) {
				candidate = cached.ClientID
			}
			if candidate == "" {
				candidate = generateClientID()
			}
			id, err = s.store.GetOrCreateDeviceID(ctx, accountID, candidate)
			if err != nil {
				return nil, err
			}
		}
	}
	if !ValidClaudeDeviceID(id) {
		return nil, fmt.Errorf("invalid persisted Claude device identity")
	}
	fp := defaultFingerprint()
	fp.ClientID, fp.IdentityVersion = id, 1
	if cached != nil {
		fp.UpdatedAt = cached.UpdatedAt
		if fp == *cached && time.Since(time.Unix(fp.UpdatedAt, 0)) < 24*time.Hour {
			return &fp, nil
		}
	}
	fp.UpdatedAt = time.Now().Unix()
	if s.cache != nil {
		if err := s.cache.SetFingerprint(ctx, accountID, &fp); err != nil {
			logger.LegacyPrintf("service.identity", "Failed to cache persisted Claude identity for account %d: %v", accountID, err)
		}
	}
	return &fp, nil
}

// ApplyFingerprint 将指纹应用到请求头（覆盖原有的x-stainless-*头）
// 使用 setHeaderRaw 保持原始大小写（如 X-Stainless-OS 而非 X-Stainless-Os）
func (s *IdentityService) ApplyFingerprint(req *http.Request, fp *Fingerprint) {
	if fp == nil {
		return
	}

	// 设置user-agent
	if fp.UserAgent != "" {
		setHeaderRaw(req.Header, "User-Agent", fp.UserAgent)
	}

	// 设置x-stainless-*头（保持与 claude.DefaultHeaders() 一致的大小写）
	if fp.StainlessLang != "" {
		setHeaderRaw(req.Header, "X-Stainless-Lang", fp.StainlessLang)
	}
	if fp.StainlessPackageVersion != "" {
		setHeaderRaw(req.Header, "X-Stainless-Package-Version", fp.StainlessPackageVersion)
	}
	if fp.StainlessOS != "" {
		setHeaderRaw(req.Header, "X-Stainless-OS", fp.StainlessOS)
	}
	if fp.StainlessArch != "" {
		setHeaderRaw(req.Header, "X-Stainless-Arch", fp.StainlessArch)
	}
	if fp.StainlessRuntime != "" {
		setHeaderRaw(req.Header, "X-Stainless-Runtime", fp.StainlessRuntime)
	}
	if fp.StainlessRuntimeVersion != "" {
		setHeaderRaw(req.Header, "X-Stainless-Runtime-Version", fp.StainlessRuntimeVersion)
	}
}

func rewriteParentSessionID(namespace string, extraFields map[string]json.RawMessage) map[string]json.RawMessage {
	if len(extraFields) == 0 {
		return extraFields
	}
	raw, ok := extraFields["parent_session_id"]
	if !ok {
		return extraFields
	}
	var parentSessionID string
	if err := json.Unmarshal(raw, &parentSessionID); err != nil || parentSessionID == "" {
		return extraFields
	}
	remapped := make(map[string]json.RawMessage, len(extraFields))
	for key, value := range extraFields {
		remapped[key] = value
	}
	parent := generateUUIDFromSeed(namespace + parentSessionID)
	remapped["parent_session_id"], _ = json.Marshal(parent)
	return remapped
}

// RewriteUserID 重写 body 中的 metadata.user_id。
// 支持旧拼接格式和新 JSON 格式的 user_id 解析，
// 根据 fingerprintUA 版本选择输出格式。
//
// 重要：此函数使用 json.RawMessage 保留其他字段的原始字节，
// 避免重新序列化导致 thinking 块等内容被修改。
func (s *IdentityService) RewriteUserID(body []byte, accountID int64, accountUUID, cachedClientID, fingerprintUA string) ([]byte, error) {
	return s.rewriteUserID(context.Background(), body, accountID, accountUUID, cachedClientID, fingerprintUA, false)
}

func (s *IdentityService) rewriteUserID(ctx context.Context, body []byte, accountID int64, accountUUID, cachedClientID, fingerprintUA string, masked bool) ([]byte, error) {
	if len(body) == 0 || accountUUID == "" || cachedClientID == "" {
		return body, nil
	}

	metadata := gjson.GetBytes(body, "metadata")
	if !metadata.Exists() || metadata.Type == gjson.Null {
		return body, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(metadata.Raw), "{") {
		return body, nil
	}

	userIDResult := metadata.Get("user_id")
	if !userIDResult.Exists() || userIDResult.Type != gjson.String {
		return body, nil
	}
	userID := userIDResult.String()
	if userID == "" {
		return body, nil
	}

	// 解析 user_id（兼容旧拼接格式和新 JSON 格式）
	parsed := ParseMetadataUserID(userID)
	if parsed == nil {
		return body, nil
	}
	// Both session and parent are derived from the same original-ID namespace.
	// Keep the account-only namespace byte-for-byte identical when masking is off.
	namespace := fmt.Sprintf("%d::", accountID)
	if masked {
		seed, err := s.cache.GetSessionMaskSeed(ctx, accountID)
		if err == nil {
			candidate := ""
			if seed == "" {
				candidate = generateRandomUUID()
			}
			// Always use the authoritative winner, including when the read seed expired.
			seed, err = s.cache.GetOrCreateSessionMaskSeed(ctx, accountID, candidate)
			if err == nil && seed == "" {
				// Expired between read and refresh: this authoritative miss permits creation.
				seed, err = s.cache.GetOrCreateSessionMaskSeed(ctx, accountID, generateRandomUUID())
			}
		}
		if err != nil {
			logger.LegacyPrintf("service.identity", "Warning: failed to refresh session mask seed for account %d: %v", accountID, err)
		} else if seed != "" {
			namespace += seed + "::"
		}
	}
	extraFields := rewriteParentSessionID(namespace, parsed.ExtraFields)
	newSessionHash := generateUUIDFromSeed(namespace + parsed.SessionID)

	// 根据客户端版本选择输出格式；保留 JSON user_id 中所有未知扩展字段。
	version := ExtractCLIVersion(fingerprintUA)
	newUserID := formatMetadataUserID(cachedClientID, accountUUID, newSessionHash, extraFields, version)
	if newUserID == userID {
		return body, nil
	}

	newBody, err := sjson.SetBytes(body, "metadata.user_id", newUserID)
	if err != nil {
		return body, nil
	}
	return newBody, nil
}

// RewriteUserIDWithMasking remaps original sessions and parents using one
// account-scoped namespace, rotated after 15 minutes without requests.
func (s *IdentityService) RewriteUserIDWithMasking(ctx context.Context, body []byte, account *Account, accountUUID, cachedClientID, fingerprintUA string) ([]byte, error) {
	return s.rewriteUserID(ctx, body, account.ID, accountUUID, cachedClientID, fingerprintUA, account.IsSessionIDMaskingEnabled())
}

// generateRandomUUID 生成随机 UUID v4 格式字符串
func generateRandomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback: 使用时间戳生成
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		b = h[:16]
	}

	// 设置 UUID v4 版本和变体位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// generateClientID 生成64位十六进制客户端ID（32字节随机数）
func generateClientID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 极罕见的情况，使用时间戳+固定值作为fallback
		logger.LegacyPrintf("service.identity", "Warning: crypto/rand.Read failed: %v, using fallback", err)
		// 使用SHA256(当前纳秒时间)作为fallback
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
}

// generateUUIDFromSeed 从种子生成确定性UUID v4格式字符串
func generateUUIDFromSeed(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	bytes := hash[:16]

	// 设置UUID v4版本和变体位
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
