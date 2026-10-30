package workspace

import (
	"bytes"
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"image"
	"image/png"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCredentialExportChecksOwnerAndKeepsClientFieldNames(t *testing.T) {
	credential := map[string]any{"email": "Owner@Example.com", "access_token": "test-access", "refresh_token": "rotated-refresh", "id_token": "test-identity", "chatgpt_account_id": "account"}
	run := reloginRun{Email: "owner@example.com"}
	result := map[string]any{"credential": credential}
	for _, format := range []string{"raw", "sub2api", "codex", "cpa", "cockpit", "9router", "axonhub", "codexmanager", "session", "portable", "refresh_token"} {
		if _, err := credentialExport(format, run, result); err != nil {
			t.Errorf("%s: %v", format, err)
		}
	}
	out, err := credentialExport("codex", run, result)
	if err != nil {
		t.Fatal(err)
	}
	auth := out.(gin.H)
	if auth["tokens"].(map[string]any)["refresh_token"] != "rotated-refresh" || auth["auth_mode"] != "chatgpt" {
		t.Fatal("invalid Codex auth format")
	}
	run.Email = "other@example.com"
	if _, err = credentialExport("raw", run, result); err == nil {
		t.Fatal("wrong owner exported")
	}
	run.Email = "owner@example.com"
	delete(credential, "refresh_token")
	if _, err = credentialExport("axonhub", run, result); err == nil {
		t.Fatal("missing refresh token was replaced with a placeholder")
	}
}

func TestTicketUnicodeTextLimit(t *testing.T) {
	p := ticketPayload{Content: strings.Repeat("字", 50000)}
	if _, err := prepareTicketPayload(&p); err != nil {
		t.Fatal(err)
	}
	p.Content += "字"
	if _, err := prepareTicketPayload(&p); err == nil {
		t.Fatal("oversize content accepted")
	}
	p.Content = "  "
	if _, err := prepareTicketPayload(&p); err == nil {
		t.Fatal("empty message accepted")
	}
}
func TestTicketImageIsDecodedResizedAndReencoded(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2200, 80))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	p := ticketPayload{}
	p.Images = append(p.Images, struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}{"test.png", base64.StdEncoding.EncodeToString(encoded.Bytes())})
	images, err := prepareTicketPayload(&p)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Width != 1920 || images[0].Size > 512*1024 {
		t.Fatalf("unexpected image: %+v", images)
	}
	config, kind, err := image.DecodeConfig(bytes.NewReader(images[0].Data))
	if err != nil || kind != "jpeg" || config.Width != 1920 {
		t.Fatalf("not normalized: %s %v", kind, err)
	}
	p.Images[0].Data = base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1) />"))
	if _, err = prepareTicketPayload(&p); err == nil {
		t.Fatal("non-image accepted")
	}
}
func TestPublicFeedsHaveFixedPathsAndBoundedQueries(t *testing.T) {
	for _, path := range []string{"/latest", "/hot", "/recommended", "/categories", "/sources/rss"} {
		if _, ok := newsPath(path); !ok {
			t.Errorf("valid path rejected: %s", path)
		}
	}
	for _, path := range []string{"/../../admin", "//example.com", "/latest/../../../login", "/favorites"} {
		if _, ok := newsPath(path); ok {
			t.Errorf("unsafe path accepted: %s", path)
		}
	}
	query, err := feedQuery(url.Values{"limit": {"99999"}, "Authorization": {"secret"}, "sourceId": {"10"}})
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("limit") != "50" || query.Has("Authorization") || query.Get("sourceId") != "10" {
		t.Fatalf("bad query: %v", query)
	}
	if _, err = feedQuery(url.Values{"cursor": {strings.Repeat("x", 2049)}}); err == nil {
		t.Fatal("oversize cursor accepted")
	}
}
func TestReloginIdentityAndModeValidation(t *testing.T) {
	email, err := normalizeEmail(" User@Example.COM ")
	if err != nil || email != "user@example.com" {
		t.Fatal(email, err)
	}
	for _, value := range []string{"User <a@b.com>", "not-email", "x@example.com\nBcc:a@b.com"} {
		if _, err := normalizeEmail(value); err == nil {
			t.Errorf("bad email accepted %q", value)
		}
	}
	for _, secret := range []loginSecret{{Email: "a@b.com", AuthMode: "unknown"}, {Email: "a@b.com", AuthMode: "password_2fa"}} {
		if validateSecret(&secret) == nil {
			t.Fatal("incomplete credentials accepted")
		}
	}
	secret := loginSecret{Email: "a@b.com", AuthMode: "email_otp"}
	if err = validateSecret(&secret); err != nil {
		t.Fatal(err)
	}
}
func TestReloginExclusionIsPerAccount(t *testing.T) {
	h := &Handler{activeRuns: map[string]bool{}}
	var successes atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if h.acquireRun("a@example.com") {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("duplicate job admission: %d", successes.Load())
	}
	if !h.acquireRun("b@example.com") {
		t.Fatal("unrelated account blocked")
	}
	h.releaseRun("a@example.com")
	if !h.acquireRun("a@example.com") {
		t.Fatal("completed job did not release admission")
	}
}
