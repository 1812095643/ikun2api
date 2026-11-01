package workspace

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var deniedImageNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("2002::/16"),
}

func publicImageIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range deniedImageNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
func imageURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || len(raw) > 8192 {
		return nil, badRequest("请提供可访问的图片网址。")
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !publicImageIP(ip) {
		return nil, &requestError{403, "只支持公网图片地址。"}
	}
	u.Fragment = ""
	return u, nil
}

func (h *Handler) imageRelay(c *gin.Context) {
	target, err := imageURL(c.Query("url"))
	if err != nil {
		fail(c, err)
		return
	}
	// 在实际拨号时校验并固定 DNS 结果，重定向也重新校验，避免 DNS 重绑定。
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, errors.New("no image address")
		}
		for _, ip := range ips {
			if !publicImageIP(ip) {
				return nil, errors.New("private image address")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many image redirects")
		}
		_, e := imageURL(req.URL.String())
		return e
	}}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "IKUN2api-ImageStudio/1.1")
	r, err := client.Do(req)
	if err != nil {
		fail(c, &requestError{502, "参考图暂时无法读取，请下载到本机后上传。"})
		return
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		fail(c, &requestError{502, "图片来源未返回图片，请更换网址。"})
		return
	}
	mime := strings.ToLower(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(mime, "image/") || mime == "image/svg+xml" {
		fail(c, badRequest("该网址返回的不是受支持的图片。"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (32<<20)+1))
	if err != nil || len(raw) > 32<<20 {
		fail(c, &requestError{413, "参考图超过 32 MB，请先缩小后上传。"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, mime, raw)
}
