package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	pkghttputil "github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"net"
	"strings"
	"syscall"
)

// Return only a finite diagnostic enum. Never export error text, hostnames,
// proxy credentials or URLs to monitor evidence.
func monitorTransportReason(c *gin.Context, err error) string {
	if c != nil && c.Request != nil && isClientCanceledTransportError(c.Request.Context(), err) {
		return "client_cancelled"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var record tls.RecordHeaderError
	if errors.As(err, &cert) || errors.As(err, &unknownCA) || errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &record) {
		return "tls"
	}
	if errors.Is(err, ErrProxyQuarantined) {
		return "proxy"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		return "proxy"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, pkghttputil.ErrStreamResponseHeaderTimeout) {
		return "timeout"
	}
	var timed net.Error
	if errors.As(err, &timed) && timed.Timeout() {
		return "timeout"
	}
	if errors.As(err, &op) && op.Op == "dial" || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "connect"
	}
	if err != nil {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "proxy authentication required") || strings.Contains(text, "socks") && strings.Contains(text, "authentication failed") {
			return "proxy"
		}
	}
	return "transport_unknown"
}
