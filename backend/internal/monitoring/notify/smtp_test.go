package notify

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSMTPUsesVerifiedTLSAndDataAcceptance(t *testing.T) {
	seed := httptest.NewTLSServer(nil)
	cert := seed.TLS.Certificates[0]
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	seed.Close()
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 mock ESMTP\r\n")
		var body strings.Builder
		for {
			line, e := r.ReadString('\n')
			if e != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				fmt.Fprint(conn, "250-mock\r\n250 AUTH PLAIN\r\n")
			case strings.HasPrefix(line, "AUTH"):
				fmt.Fprint(conn, "235 accepted\r\n")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				fmt.Fprint(conn, "250 accepted\r\n")
			case strings.HasPrefix(line, "DATA"):
				fmt.Fprint(conn, "354 continue\r\n")
				for {
					line, e = r.ReadString('\n')
					if e != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					body.WriteString(line)
				}
				fmt.Fprint(conn, "250 queued\r\n")
				received <- body.String()
			case strings.HasPrefix(line, "QUIT"):
				return // Closing after DATA acceptance must not trigger a resend.
			}
		}
	}()
	smtp := &SMTP{Address: listener.Addr().String(), Username: "test-user", Password: "test-password", From: "ops@example.test", ImplicitTLS: true, tlsConfig: &tls.Config{ServerName: "example.com", RootCAs: roots, MinVersion: tls.VersionTLS12}}
	d := domain.Delivery{ID: "delivery-1", Target: "recipient@example.test", Payload: domain.Incident{Severity: "P1", Status: "firing", Explanation: domain.Explanation{Conclusion: "上游异常", Facts: []string{"状态码503"}}}}
	id, err := smtp.Send(context.Background(), d, "", "")
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	select {
	case text := <-received:
		if !strings.Contains(text, "状态码503") || strings.Contains(text, "test-password") {
			t.Fatal("unsafe or incomplete email")
		}
	case <-time.After(time.Second):
		t.Fatal("DATA not received")
	}
}
func TestSMTPRejectsPlaintextServer(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprint(conn, "220 mock ESMTP\r\n")
		bufio.NewReader(conn).ReadString('\n')
		fmt.Fprint(conn, "250 mock\r\n")
	}()
	s := &SMTP{Address: listener.Addr().String(), From: "a@example.test"}
	_, err := s.Send(context.Background(), domain.Delivery{Target: "b@example.test"}, "", "")
	if err == nil || err.Error() != "smtp_tls_required" {
		t.Fatal(err)
	}
}
