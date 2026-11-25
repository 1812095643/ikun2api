package channels

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

type rewrite struct{ to *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestSenderPicksUpNewCredentialsWithoutRestart(t *testing.T) {
	var mu sync.Mutex
	tokens := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "tenant_access_token/internal") {
			buf := make([]byte, 512)
			n, _ := r.Body.Read(buf)
			body := string(buf[:n])
			for _, id := range []string{"cli_first0000001", "cli_second000002"} {
				if strings.Contains(body, id) {
					tokens[id]++
					fmt.Fprintf(w, `{"code":0,"tenant_access_token":"tok-%s","expire":7200}`, id)
					return
				}
			}
			fmt.Fprint(w, `{"code":10014,"msg":"bad"}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_x1"}}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	env := Settings{Feishu: Feishu{Domain: "feishu", AppID: "cli_first0000001", AppSecret: "s1"}}
	reg, err := Open(s, filepath.Join(dir, "key"), env)
	if err != nil {
		t.Fatal(err)
	}
	reg.Transport = rewrite{u}
	sender := Sender{Registry: reg, Channel: "feishu"}
	d := notify.TestDelivery("a", "feishu", "oc_ops1", time.Now())
	ctx := context.Background()
	for n := 0; n < 2; n++ {
		if _, err = sender.Send(ctx, d, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = reg.Save(ctx, Settings{Feishu: Feishu{Domain: "feishu", AppID: "cli_second000002", AppSecret: "s2"}}, "admin:1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = sender.Send(ctx, d, "", ""); err != nil {
		t.Fatal(err)
	}
	if tokens["cli_first0000001"] != 1 || tokens["cli_second000002"] != 1 {
		t.Fatalf("token must be cached per credential and refreshed on change: %v", tokens)
	}
	// Email not configured: permanent failure, not a crash.
	if _, err = (Sender{Registry: reg, Channel: "email"}).Send(ctx, notify.TestDelivery("b", "email", "a@b.c", time.Now()), "", ""); err == nil {
		t.Fatal("unconfigured email must fail")
	}
}

func TestKeyFileIsReusedAndCorruptKeyRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := filepath.Join(dir, "key")
	a, err := Open(s, key, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := Settings{Feishu: Feishu{Domain: "feishu"}, Email: Email{Host: "smtp.qq.com", Port: 465, Security: "ssl", Username: "u@qq.com", Password: "pw", From: "u@qq.com"}}
	if err = a.Save(ctx, in, "admin:1", time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := Open(s, key, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	got, src, err := b.Load(ctx)
	if err != nil || src != "page" || got.Email.Password != "pw" {
		t.Fatal("reopened registry must decrypt with the same key", got, src, err)
	}
	other, err := Open(s, filepath.Join(dir, "other"), Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = other.Load(ctx); err == nil {
		t.Fatal("a different key must not decrypt")
	}
}
