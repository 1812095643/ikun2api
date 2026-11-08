package middleware

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/MACOS-DO/sub4api/internal/pkg/ip"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

// Multi-group behavior follows kiss-kedaya/sub2api (c33708f). Re-entering the
// existing route with a server-owned group binding keeps every IKUN-specific
// policy, scheduler, protocol adapter and billing path in one implementation.
// There is at most one nested dispatch; an attempt can never start another loop.
type smartRouteAttemptKey struct{}
type smartRouteLookupKey struct{}
type smartRouteLookup struct {
	credential string
	key        *service.APIKey
	err        error
}

// The coordinator performs one cache lookup. Ordinary authentication still owns
// every validation; sharing this request-local result avoids doubling lookup
// pressure for existing single-group keys and for each fallback candidate.
func lookupAPIKeyForRequest(c *gin.Context, keys SmartRouteKeyService, credential string) (*service.APIKey, error) {
	if cached, _ := c.Request.Context().Value(smartRouteLookupKey{}).(*smartRouteLookup); cached != nil && cached.credential == credential {
		return cached.key, cached.err
	}
	return keys.GetByKey(c.Request.Context(), credential)
}

type smartRouteAttempt struct {
	groupID            int64
	retryable, skipped bool
	selection          *service.APIKeyRouteSelection
}

type SmartRouteKeyService interface {
	GetByKey(context.Context, string) (*service.APIKey, error)
	BindAPIKeyRequestGroup(context.Context, *service.APIKey, int64) (*service.APIKey, error)
}

type SmartRouteMatcher func(*gin.Context, *service.APIKey) (bool, error)

// APIKeySmartRouting only retries local group denials or lack of schedulable
// accounts before output. A scheduler-owned exhaustion result can advance the
// group after its account attempts fail; an arbitrary upstream error cannot.
// Successful output and established WebSocket/SSE are never replayed.
func APIKeySmartRouting(engine http.Handler, keys SmartRouteKeyService, configs ...*config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if attempt, ok := c.Request.Context().Value(smartRouteAttemptKey{}).(*smartRouteAttempt); ok {
			c.Next()
			attempt.retryable = smartRouteCanContinue(c, attempt)
			return
		}
		if keys == nil || engine == nil || apiKeyHeadersTooLarge(c) {
			c.Next()
			return
		}
		if c.Query("api_key") != "" || (c.Query("key") != "" && !allowGoogleQueryKey(c.Request.URL.Path)) {
			c.Next()
			return
		}
		credential := smartRouteCredential(c)
		if credential == "" || len(credential) > service.MaxAPIKeyCredentialBytes {
			c.Next()
			return
		}
		if concrete, ok := keys.(*service.APIKeyService); ok && rejectInvalidAuthAbuse(c, concrete) {
			c.Next()
			return
		}
		key, err := keys.GetByKey(c.Request.Context(), credential)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), smartRouteLookupKey{}, &smartRouteLookup{credential: credential, key: key, err: err}))
		if err != nil || key == nil || len(key.CandidateGroupIDs()) < 2 {
			c.Next()
			return
		}
		if key.User == nil || !key.User.IsActive() || (!key.IsActive() && key.Status != service.StatusAPIKeyExpired && key.Status != service.StatusAPIKeyQuotaExhausted) {
			c.Next()
			return
		}
		if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
			trust := len(configs) > 0 && configs[0] != nil && configs[0].TrustForwardedIPForAPIKeyACL()
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(ip.GetSecurityClientIP(c, trust), key.CompiledIPWhitelist, key.CompiledIPBlacklist)
			if !allowed {
				c.Next()
				return
			}
		}
		// Let the regular auth middleware produce all credential/IP/user/key-limit
		// errors. No catalog or upstream request is made before that middleware.
		// Keep wire bytes intact. Each normal attempt decodes its own body,
		// so compressed and multipart requests keep their original limits and
		// one group's normalization cannot leak into a later route.
		rawRequest := c.Request.Clone(c.Request.Context())
		rawRequest.Header.Del("Content-Encoding")
		body, err := httputil.ReadRequestBodyWithPrealloc(rawRequest)
		if err != nil {
			if !RejectRequestBodyReadError(c, "middleware.smart_route", err) {
				AbortWithRequestBodyReadFailure(c, "middleware.smart_route", err)
			}
			return
		}
		if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			c.Request = c.Request.WithContext(service.WithAPIKeyRouteRPMReceipts(c.Request.Context()))
		}
		discovery := c.Request.Method == http.MethodGet && strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/models")
		var best, last *smartRouteWriter
		var catalogs []*smartRouteWriter
		for _, groupID := range key.CandidateGroupIDs() {
			if c.Request.Context().Err() != nil {
				c.Abort()
				return
			}
			attempt := &smartRouteAttempt{groupID: groupID}
			attemptCtx, selection := service.WithAPIKeyRouteSelection(c.Request.Context())
			attempt.selection = selection
			// Each candidate owns its decoded/rewritten copies; releasing one
			// candidate must not release the original bytes used by later groups.
			attemptCtx, releaseBody := httputil.WithRequestBodyMemoryScope(attemptCtx)
			req := c.Request.Clone(context.WithValue(attemptCtx, smartRouteAttemptKey{}, attempt))
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
			writer := &smartRouteWriter{target: c.Writer, header: make(http.Header), bufferAll: discovery}
			func() {
				defer releaseBody()
				engine.ServeHTTP(writer, req)
			}()
			_ = req.Body.Close()
			if writer.committed {
				c.Abort()
				return
			}
			last = writer
			if discovery && writer.status >= 200 && writer.status < 300 {
				catalogs = append(catalogs, writer)
				continue
			}
			if !attempt.skipped {
				best = writer
			}
			if !attempt.retryable {
				writer.commit()
				c.Abort()
				return
			}
		}
		if len(catalogs) > 0 {
			mergeSmartRouteCatalogs(catalogs).commit()
		} else if best != nil {
			best.commit()
		} else if last != nil {
			last.commit()
		}
		c.Abort()
	}
}

// Called inside ordinary authentication, after the credential and owner ACL.
// The group ID is only present in an internal Go context, never an HTTP header.
func bindSmartRouteGroup(c *gin.Context, keys SmartRouteKeyService, key *service.APIKey) (*service.APIKey, error) {
	attempt, _ := c.Request.Context().Value(smartRouteAttemptKey{}).(*smartRouteAttempt)
	if attempt == nil {
		return key, nil
	}
	routed, err := keys.BindAPIKeyRequestGroup(c.Request.Context(), key, attempt.groupID)
	if errors.Is(err, service.ErrGroupNotFound) {
		cloned := *key
		id := attempt.groupID
		cloned.GroupID = &id
		cloned.Group = nil
		return &cloned, nil
	}
	return routed, err
}

// APIKeySmartRouteGuard runs after normal authentication so model catalogs and
// group membership cannot be observed with invalid credentials.
func APIKeySmartRouteGuard(matches SmartRouteMatcher) gin.HandlerFunc {
	return func(c *gin.Context) {
		attempt, _ := c.Request.Context().Value(smartRouteAttemptKey{}).(*smartRouteAttempt)
		if attempt == nil || matches == nil {
			c.Next()
			return
		}
		key, ok := GetAPIKeyFromContext(c)
		if !ok {
			c.Next()
			return
		}
		match, err := matches(c, key)
		if err != nil {
			if RejectRequestBodyReadError(c, "middleware.smart_route_guard", err) {
				return
			}
			if !errors.Is(err, service.ErrSchedulerCacheNotReady) {
				AbortWithRequestBodyReadFailure(c, "middleware.smart_route_guard", err)
				return
			}
			status, message := http.StatusBadRequest, "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
			}
			if errors.Is(err, service.ErrSchedulerCacheNotReady) {
				status, message = http.StatusServiceUnavailable, "Failed to resolve key route"
			}
			groupModelAllowlistErrorWriter(c)(c, status, message)
			c.Abort()
			return
		}
		if !match {
			attempt.skipped = true
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
			groupModelAllowlistErrorWriter(c)(c, http.StatusNotFound, "No matching model or protocol in this group")
			c.Abort()
			return
		}
		c.Next()
	}
}

func smartRouteCanContinue(c *gin.Context, attempt *smartRouteAttempt) bool {
	if c.Request.Context().Err() != nil {
		return false
	}
	// A real upstream failure, including a 400 validation response, must retain
	// its original result. Merely seeing 503 is never permission to replay.
	if attempt.selection.Exhausted() && c.Writer.Status() >= 400 {
		return true
	}
	if accountID, exists := c.Get("ops_account_id"); exists && accountID != nil {
		return false
	}
	if attempt.skipped {
		return true
	}
	if value, _ := c.Get(ingressRejectReasonContextKey); value != nil {
		switch value.(type) {
		case IngressRejectReason:
			switch value.(IngressRejectReason) {
			case IngressRejectGroupDeleted, IngressRejectGroupDisabled, IngressRejectGroupNotAllowed, IngressRejectModelNotAllowed:
				return true
			}
		}
	}
	if value, _ := c.Get(service.OpsClientRestrictionKey); value == true {
		return true
	}
	if value, _ := c.Get("ops_routing_capacity_limited"); value == true {
		return true
	}
	switch service.OpsClientBusinessLimitedReason(c) {
	case service.OpsClientBusinessLimitedReasonLocalModelConfiguration, service.OpsClientBusinessLimitedReasonLocalFeatureGate:
		return true
	}
	return false
}

// Successful HTTP and streaming output pass through immediately. Only error
// replies (and model discovery) are buffered, with a strict 2 MiB memory cap.
type smartRouteWriter struct {
	target               http.ResponseWriter
	header               http.Header
	body                 bytes.Buffer
	status               int
	bufferAll, committed bool
}

func (w *smartRouteWriter) Header() http.Header { return w.header }
func (w *smartRouteWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status < 400 && !w.bufferAll {
		w.commit()
	}
}
func (w *smartRouteWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.committed {
		return w.target.Write(p)
	}
	if w.body.Len()+len(p) > 2<<20 {
		w.commit()
		return w.target.Write(p)
	}
	return w.body.Write(p)
}
func (w *smartRouteWriter) commit() {
	if w.committed {
		return
	}
	w.committed = true
	for k, values := range w.header {
		w.target.Header()[k] = append([]string(nil), values...)
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.target.WriteHeader(status)
	if w.body.Len() > 0 {
		_, _ = w.target.Write(w.body.Bytes())
		w.body.Reset()
	}
}
func (w *smartRouteWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.committed {
		return
	}
	if f, ok := w.target.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *smartRouteWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// The actual upgrade handler owns the handshake. Do not send HTTP 200 here.
	w.committed = true
	return w.target.(http.Hijacker).Hijack()
}
func (w *smartRouteWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.target.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	return make(chan bool)
}
func (w *smartRouteWriter) Unwrap() http.ResponseWriter { return w.target }

func mergeSmartRouteCatalogs(catalogs []*smartRouteWriter) *smartRouteWriter {
	first := catalogs[0]
	var base map[string]json.RawMessage
	if json.Unmarshal(first.body.Bytes(), &base) != nil {
		return first
	}
	field := "data"
	if _, ok := base[field]; !ok {
		field = "models"
	}
	var models []json.RawMessage
	seen := map[string]bool{}
	for _, catalog := range catalogs {
		var payload map[string]json.RawMessage
		if json.Unmarshal(catalog.body.Bytes(), &payload) != nil {
			continue
		}
		var rows []json.RawMessage
		if json.Unmarshal(payload[field], &rows) != nil {
			continue
		}
		for _, row := range rows {
			var identity struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Slug string `json:"slug"`
			}
			if json.Unmarshal(row, &identity) != nil {
				continue
			}
			id := identity.ID
			if id == "" {
				id = identity.Name
			}
			if id == "" {
				id = identity.Slug
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			models = append(models, row)
		}
	}
	if models == nil {
		return first
	}
	base[field], _ = json.Marshal(models)
	if _, ok := base["has_more"]; ok {
		base["has_more"] = json.RawMessage("false")
	}
	if _, ok := base["nextPageToken"]; ok {
		delete(base, "nextPageToken")
	}
	merged, err := json.Marshal(base)
	if err != nil {
		return first
	}
	first.body.Reset()
	_, _ = first.body.Write(merged)
	first.header.Del("Content-Encoding")
	first.header.Set("Content-Length", strconv.Itoa(len(merged)))
	return first
}

func smartRouteCredential(c *gin.Context) string {
	if strings.Contains(c.Request.URL.Path, "/v1beta/") {
		return extractAPIKeyForGoogle(c)
	}
	if auth := c.GetHeader("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return ""
		}
		if value := strings.TrimSpace(parts[1]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(c.GetHeader("x-api-key")); value != "" {
		return value
	}
	return strings.TrimSpace(c.GetHeader("x-goog-api-key"))
}

// BindAPIKeySmartRouteContext updates only the current connection's selected
// group. HTTP callers use the normal auth middleware; WebSocket knows its
// model only after reading response.create and uses this binding once.
func BindAPIKeySmartRouteContext(c *gin.Context, key *service.APIKey) {
	c.Set(string(ContextKeyAPIKey), key)
	SetOpsFallbackAPIKey(c, key)
	setGroupContext(c, key.Group)
}
