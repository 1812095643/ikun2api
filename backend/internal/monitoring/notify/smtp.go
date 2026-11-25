package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type SMTP struct {
	Address, Username, Password, From string
	ImplicitTLS                       bool
	// Diagnose keeps the server's own error text for the admin "send test" button.
	Diagnose  bool
	tlsConfig *tls.Config
}

func (s *SMTP) Send(ctx context.Context, d domain.Delivery, _ string, base string) (id string, err error) {
	// SMTP exceptions can include usernames/server banners; classify before returning.
	defer func() {
		if err != nil {
			var known *Failure
			if s.Diagnose {
				msg := err.Error()
				if r := []rune(msg); len(r) > 300 {
					msg = string(r[:300]) + "…"
				}
				err = &AdminError{"邮件发送失败：" + msg}
				return
			}
			if !errors.As(err, &known) {
				permanent := false
				var response *textproto.Error
				if errors.As(err, &response) {
					permanent = response.Code >= 500
				}
				err = &Failure{Class: "smtp_transport_or_rejected", Permanent: permanent}
			}
		}
	}()
	host, _, err := net.SplitHostPort(s.Address)
	if err != nil {
		return "", &Failure{Class: "smtp_configuration_invalid", Permanent: true}
	}
	from, e := mail.ParseAddress(s.From)
	to, e2 := mail.ParseAddress(d.Target)
	if e != nil || e2 != nil || strings.ContainsAny(s.From+d.Target, "\r\n") {
		return "", &Failure{Class: "mail_address_invalid", Permanent: true}
	}
	cfg := s.tlsConfig
	if cfg == nil {
		cfg = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var conn net.Conn
	if s.ImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: cfg}).DialContext(ctx, "tcp", s.Address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.Address)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if at, ok := ctx.Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if !s.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "", &Failure{Class: "smtp_tls_required", Permanent: true}
		}
		if err = client.StartTLS(cfg); err != nil {
			return "", err
		}
	}
	if s.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return "", err
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return "", err
	}
	if err = client.Rcpt(to.Address); err != nil {
		return "", err
	}
	writer, err := client.Data()
	if err != nil {
		return "", err
	}
	messageID := fmt.Sprintf("<%s@ops-monitor.local>", d.ID)
	text := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", from.String(), to.String(), mime.QEncoding.Encode("utf-8", "心链词元异常通知 "+d.Payload.Severity+" "+statusText(d.Payload.Status)), time.Now().Format(time.RFC1123Z), messageID, strings.ReplaceAll(Plain(d.Payload, base), "\n", "\r\n"))
	if _, err = writer.Write([]byte(text)); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	// Acceptance is the DATA final response. A later QUIT failure must not resend.
	_ = client.Quit()
	return messageID, nil
}
