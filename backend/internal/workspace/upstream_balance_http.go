package workspace

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/proxyurl"
	"github.com/MACOS-DO/sub4api/internal/pkg/proxyutil"
)

func normalizeUpstreamBalanceURL(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", badRequest("上游地址过长。")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", badRequest("上游地址必须是无账号、参数和片段的 HTTP(S) 地址。")
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if strings.ContainsAny(hostname, "%\\\r\n ") || hostname == "" {
		return "", badRequest("上游主机名无效。")
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", badRequest("上游端口无效。")
		}
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	u.Host = hostname
	if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	}
	// Keep deployment subpaths, normalize only conventional API version suffixes.
	clean := strings.TrimRight(path.Clean("/"+u.Path), "/")
	for _, suffix := range []string{"/v1beta/openai", "/v1beta", "/v1"} {
		if strings.HasSuffix(clean, suffix) {
			clean = strings.TrimSuffix(clean, suffix)
			break
		}
	}
	if strings.ContainsAny(clean, "\\\r\n\x00") || strings.Contains(clean, "..") {
		return "", badRequest("上游路径无效。")
	}
	u.Path = clean
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func balancePublicIP(addr netip.Addr, allowPrivate bool) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	if addr.IsPrivate() {
		return allowPrivate
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "0.0.0.0/8", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return false
		}
	}
	return true
}

type balancePinnedTransport struct {
	transport *http.Transport
	root      *url.URL
	ip        netip.Addr
}

func (p *balancePinnedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != p.root.Scheme || req.URL.Host != p.root.Host {
		return nil, balanceError("余额连接目标不匹配。")
	}
	clone := req.Clone(req.Context())
	u := *req.URL
	clone.URL = &u
	clone.Host = req.URL.Host
	port := req.URL.Port()
	if port == "" {
		port = "443"
		if req.URL.Scheme == "http" {
			port = "80"
		}
	}
	clone.URL.Host = net.JoinHostPort(p.ip.String(), port)
	return p.transport.RoundTrip(clone)
}
func (h *Handler) upstreamBalanceClient(ctx context.Context, root string, proxyID *int64) (*http.Client, func(), error) {
	u, _ := url.Parse(root)
	if u.Scheme == "http" && (h.cfg == nil || !h.cfg.Security.URLAllowlist.Enabled || !h.cfg.Security.URLAllowlist.AllowInsecureHTTP) {
		return nil, nil, balanceError("公网余额接口须使用 HTTPS；HTTP 需由服务器显式允许。")
	}
	allowPrivate := h.cfg != nil && h.cfg.Security.URLAllowlist.Enabled && h.cfg.Security.URLAllowlist.AllowPrivateHosts
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, nil, balanceError("无法解析上游地址。")
	}
	for _, ip := range ips {
		if !balancePublicIP(ip, allowPrivate) {
			return nil, nil, balanceError("余额查询已拦截本机、内网或保留地址。")
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.MaxConnsPerHost = 1
	tr.MaxIdleConnsPerHost = 1
	tr.DisableKeepAlives = true
	tr.ResponseHeaderTimeout = 12 * time.Second
	tr.TLSHandshakeTimeout = 8 * time.Second
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	// Direct connections are pinned to the validated addresses, including IPv6;
	// a second DNS lookup cannot turn a public host into an internal target.
	tr.DialContext = func(c context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || !strings.EqualFold(host, u.Hostname()) {
			return nil, balanceError("上游连接目标不匹配。")
		}
		var last error
		for _, ip := range ips {
			conn, e := dialer.DialContext(c, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	var transport http.RoundTripper = tr
	if proxyID != nil {
		p, e := h.admin.GetProxy(ctx, *proxyID)
		if e != nil || p == nil || !p.IsActive() || p.IsExpired(time.Now()) {
			return nil, nil, balanceError("账号代理不可用，请先检查账号管理中的代理设置。")
		}
		_, pu, e := proxyurl.Parse(p.URL())
		if e != nil {
			return nil, nil, balanceError("账号代理配置无效。")
		}
		// Pin the validated upstream even through HTTP CONNECT / SOCKS. Preserve the
		// original Host and TLS SNI; never fall back to a direct connection.
		tr.DialContext = dialer.DialContext
		if e = proxyutil.ConfigureTransportProxy(tr, pu); e != nil {
			return nil, nil, balanceError("无法创建账号代理连接。")
		}
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
		transport = &balancePinnedTransport{transport: tr, root: u, ip: ips[0]}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return client, tr.CloseIdleConnections, nil
}

func balanceGetJSON(ctx context.Context, client *http.Client, root, endpoint, authorization string, userID int64) (map[string]any, error) {
	if endpoint != "v1/usage" && endpoint != "api/status" && endpoint != "api/usage/token/" && endpoint != "api/user/self" {
		return nil, balanceError("余额接口路径无效。")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/"+endpoint, nil)
	if err != nil {
		return nil, balanceError("无法创建余额请求。")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "IKUN2api-BalanceMonitor/1")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if userID > 0 {
		req.Header.Set("New-Api-User", strconv.FormatInt(userID, 10))
	}
	guarded := *client
	guarded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	res, err := guarded.Do(req)
	if err != nil {
		return nil, balanceError("余额查询连接失败或超时，请检查地址和代理。")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, balanceStatusError(res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, balanceError("余额响应无法读取或超过大小限制。")
	}
	var p map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&p) != nil || p == nil {
		return nil, balanceError("余额接口未返回 JSON，请检查站点类型。")
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return nil, balanceError("余额接口返回了无效的 JSON。")
	}
	return p, nil
}

func queryUpstreamBalance(ctx context.Context, client *http.Client, root, key, wallet string, c upstreamBalanceConfig) (upstreamBalanceReading, error) {
	if key == "" && wallet == "" {
		return upstreamBalanceReading{}, balanceError("账号未配置 API Key。")
	}
	if validateWalletToken(key) != nil || validateWalletToken(wallet) != nil {
		return upstreamBalanceReading{}, balanceError("账号 Key 格式无效。")
	}
	if (c.Kind == "sub2api" || c.Kind == "auto") && wallet == "" {
		p, err := balanceGetJSON(ctx, client, root, "v1/usage", "Bearer "+key, 0)
		if err == nil {
			var r upstreamBalanceReading
			r, err = parseSubUpstreamBalance(p)
			if err == nil {
				return r, nil
			}
		}
		if c.Kind == "sub2api" {
			return upstreamBalanceReading{}, err
		}
		if ctx.Err() != nil {
			return upstreamBalanceReading{}, balanceError("余额查询超时。")
		}
	}
	status, err := balanceGetJSON(ctx, client, root, "api/status", "", 0)
	if err != nil {
		return upstreamBalanceReading{}, err
	}
	if wallet != "" {
		return queryNewAPIWallet(ctx, client, root, wallet, c, status)
	}
	// Prefer a model API key, with the upstream's explicit wallet-query permission.
	p, err := balanceGetJSON(ctx, client, root, "api/usage/token/", "Bearer "+key, 0)
	if err == nil {
		var r upstreamBalanceReading
		r, err = parseNewAPITokenBalance(p, status)
		if err == nil {
			return r, nil
		}
	}
	// Some existing accounts store a personal wallet access token directly.
	return queryNewAPIWallet(ctx, client, root, key, c, status)
}
func queryNewAPIWallet(ctx context.Context, client *http.Client, root, token string, c upstreamBalanceConfig, status map[string]any) (upstreamBalanceReading, error) {
	bearer := c.AuthMode != "legacy"
	auth := token
	if bearer {
		auth = "Bearer " + token
	}
	p, err := balanceGetJSON(ctx, client, root, "api/user/self", auth, c.NewAPIUserID)
	if err == nil {
		var r upstreamBalanceReading
		r, err = parseNewAPIWalletBalance(p, status)
		if err == nil {
			return r, nil
		}
	}
	var qe *balanceQueryError
	if c.AuthMode == "auto" && c.NewAPIUserID > 0 && errors.As(err, &qe) && (qe.status == 401 || qe.status == 403) {
		p, err = balanceGetJSON(ctx, client, root, "api/user/self", token, c.NewAPIUserID)
		if err == nil {
			return parseNewAPIWalletBalance(p, status)
		}
	}
	return upstreamBalanceReading{}, err
}
