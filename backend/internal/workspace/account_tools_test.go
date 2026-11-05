package workspace

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestVaultFiveColumnImportPreservesSecrets(t *testing.T) {
	raw := "One@example.com----  test-password+ : ^  ----sk-ant-sid02-TEST_ONLY----Backup@example.net----aux-+ / :\nTwo@example.com----pw----sk-ant-sid02-SECOND_ONLY--------"
	rows := parseVaultImport(raw)
	if len(rows) != 2 {
		t.Fatal("expected two rows")
	}
	for _, r := range rows {
		if r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	if rows[0].Entry.Password != "  test-password+ : ^  " || rows[0].Entry.AuxiliaryPassword != "aux-+ / :" || rows[0].Entry.SessionKey != "sk-ant-sid02-TEST_ONLY" || rows[0].Entry.AuxiliaryEmail != "Backup@example.net" {
		t.Fatal("fields moved or secret modified")
	}
	if rows[1].Entry.AuxiliaryEmail != "" {
		t.Fatal("empty column moved")
	}
}
func TestVaultLabelImportSeparatesMailURLAndAuxiliary(t *testing.T) {
	rows := parseVaultImport("Claude Pro成品\n--------------------------------\n邮箱账号：One@example.com\n邮箱密码：test-!pass\n辅助邮箱：Backup@example.net\n辅邮密码：aux-only\nSessionKey：sk-ant-sid02-TEST_ONLY\n邮箱地址：https://outlook.live.com/mail/\n辅邮地址：https://mail.example.net/\n\nClaude Pro成品\n邮箱账号：Two@example.com\n邮箱密码：test2")
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	if rows[0].Error != "" || rows[1].Error != "" {
		t.Fatal("valid label import rejected")
	}
	e := rows[0].Entry
	if e.MailURL != "https://outlook.live.com/mail/" || e.AuxiliaryURL != "https://mail.example.net/" || e.AuxiliaryEmail != "Backup@example.net" || e.Title != "Claude Pro成品" {
		t.Fatal("wrong mapping")
	}
}
func TestVaultImportRejectsAmbiguityAndUnsafeData(t *testing.T) {
	tests := []string{
		"a@example.com----pw----2FA_NOT_SESSION----b@example.com----pw",
		"a@example.com----pw----sk-ant-sid02-TEST----b@example.com----pw----extra",
		"邮箱账号：a@example.com\n邮箱密码：pw\n邮箱密码：second",
		"邮箱账号：a@example.com\n邮箱密码：pw\n未知字段：not-for-logs",
		"邮箱账号：a@example.com\n邮箱密码：pw\nunmapped trailing text",
		`[{"email":"a@example.com","password":"x","surprise":"secret"}]`,
		"邮箱账号：a@example.com\n邮箱地址：javascript:alert(1)",
	}
	for i, raw := range tests {
		rows := parseVaultImport(raw)
		bad := false
		for _, r := range rows {
			bad = bad || r.Error != ""
		}
		if !bad {
			t.Fatalf("ambiguous fixture %d silently accepted", i)
		}
	}
}
func TestVaultJSONAndCSVQuotedPasswords(t *testing.T) {
	for _, raw := range []string{
		`[{"email":"a@example.com","password":"  keep , spaces  ","auxiliary_email":"b@example.com"}]`,
		"email,password,auxiliary_email\r\na@example.com,\"  keep , spaces  \",b@example.com\r\n",
		"邮箱\t邮箱密码\t辅助邮箱\na@example.com\t  keep , spaces  \tb@example.com\n",
	} {
		rows := parseVaultImport(raw)
		if len(rows) != 1 || rows[0].Error != "" || rows[0].Entry.Password != "  keep , spaces  " {
			t.Fatal("quoted secret not preserved")
		}
	}
}

func TestVaultAutomaticBulkLayoutsPreserveFields(t *testing.T) {
	for _, raw := range []string{
		"a@example.test\t  pw + :  \tsk-ant-sid02-TEST\tb@example.test\taux!\nc@example.test\tpw2\tsk-ant-sid02-TEST2\td@example.test\taux2",
		"a@example.test,\"  pw + :  \",sk-ant-sid02-TEST,b@example.test,aux!\nc@example.test,pw2,sk-ant-sid02-TEST2,d@example.test,aux2",
		"a@example.test----  pw + :  ----b@example.test----aux!----sk-ant-sid02-TEST\nc@example.test----pw2----d@example.test----aux2----sk-ant-sid02-TEST2",
		"登录邮箱：a@example.test\n邮箱密码：  pw + :  \nSessionKey：sk-ant-sid02-TEST\n辅助邮箱：b@example.test\n辅邮密码：aux!\n登录邮箱：c@example.test\n邮箱密码：pw2\nSessionKey：sk-ant-sid02-TEST2\n辅助邮箱：d@example.test\n辅邮密码：aux2",
	} {
		rows := parseVaultImport(raw)
		if len(rows) != 2 {
			t.Fatalf("expected two automatic rows, got %d", len(rows))
		}
		for _, row := range rows {
			if row.Error != "" {
				t.Fatal(row.Error)
			}
		}
		if rows[0].Entry.Email != "a@example.test" || rows[0].Entry.AuxiliaryEmail != "b@example.test" || rows[0].Entry.SessionKey != "sk-ant-sid02-TEST" || rows[0].Entry.AuxiliaryPassword != "aux!" {
			t.Fatal("bulk fields moved")
		}
	}
	rows := parseVaultImport("邮箱：a@example.test\n密码：keep----literal\n邮箱：b@example.test\n密码：next")
	if len(rows) != 2 || rows[0].Error != "" || rows[0].Entry.Password != "keep----literal" {
		t.Fatal("labeled password was treated as a delimiter row")
	}
	rows = parseVaultImport("a@example.test----pw----recovery@example.test----aux\nb@example.test----pw----sk-ant-api03-TEST")
	if len(rows) != 2 || rows[0].Error != "" || rows[1].Error != "" || rows[0].Entry.AuxiliaryPassword != "aux" || rows[1].Entry.APIKey != "sk-ant-api03-TEST" {
		t.Fatal("unambiguous shorter layouts failed")
	}
	rows = parseVaultImport("a@example.test\t keep spaces  ")
	if len(rows) != 1 || rows[0].Error != "" || rows[0].Entry.Password != " keep spaces  " {
		t.Fatal("headerless final secret was trimmed")
	}
}
func TestVaultAuthenticatedEncryptionAndIdentityBinding(t *testing.T) {
	v, err := newVaultCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := vaultSecret{Email: "a@example.com", Password: "test only", SessionKey: "sk-ant-sid02-TEST_ONLY"}
	a, _ := v.seal("row-a", s)
	b, _ := v.seal("row-a", s)
	if a == b || strings.Contains(a, s.Password) {
		t.Fatal("nonce reuse or plaintext")
	}
	out, err := v.open("row-a", v.keyID, a)
	if err != nil || out != s {
		t.Fatal("round trip")
	}
	if _, err = v.open("row-b", v.keyID, a); err == nil {
		t.Fatal("swapped ciphertext accepted")
	}
	if _, err = v.open("row-a", "wrong-key", a); err == nil {
		t.Fatal("wrong key accepted")
	}
	changed := []byte(a)
	changed[len(changed)/2] = '!'
	if _, err = v.open("row-a", v.keyID, string(changed)); err == nil {
		t.Fatal("tamper accepted")
	}
	if v.index("claude", s.Email) == v.index("openai", s.Email) {
		t.Fatal("index not platform-separated")
	}
}
func TestVaultFillAndReplaceKeepMissingFields(t *testing.T) {
	old := vaultSecret{Email: "a@example.com", Password: "old", SessionKey: "sk-ant-sid02-TEST"}
	fresh := vaultSecret{Email: "a@example.com", Password: "new"}
	if out := vaultMerge(old, fresh); out.Password != "old" || out.SessionKey == "" {
		t.Fatal("fill overwrote password")
	}
	if out := vaultMerge(fresh, old); out.Password != "new" || out.SessionKey == "" {
		t.Fatal("replace erased missing key")
	}
}
func TestVaultAdminAndOriginGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	cases := []struct {
		auth          string
		admin         bool
		origin, fetch string
		want          int
	}{
		{"jwt", true, "", "", 204}, {"jwt", false, "", "", 403}, {"admin_api_key", true, "", "", 403},
		{"jwt", true, "https://evil.example", "", 403}, {"jwt", true, "https://panel.example", "same-origin", 204}, {"jwt", true, "", "cross-site", 403},
	}
	for _, tc := range cases {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("workspace_admin", tc.admin); c.Set("auth_method", tc.auth) })
		r.GET("/", h.accountToolsAdmin, func(c *gin.Context) { c.Status(204) })
		req := httptest.NewRequest("GET", "https://panel.example/", nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.fetch)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("guard status %d expected %d", w.Code, tc.want)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("sensitive response cacheable")
		}
	}
}
func TestVaultSensitiveFailClosed(t *testing.T) {
	r := gin.New()
	h := &Handler{}
	r.POST("/", h.vaultSensitive, func(c *gin.Context) { c.Status(204) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 503 {
		t.Fatal("missing security dependency allowed read")
	}
}
func TestWarmupCrossMidnightAndNoBackfill(t *testing.T) {
	c := warmupConfig{Timezone: "Asia/Shanghai", Days: []int{1}, Windows: []warmupWindow{{Start: "23:00", End: "01:00", Rounds: 2}}}
	at := func(s string) time.Time {
		v, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if len(warmupOccurrences(c, at("2026-11-02T23:00:15+08:00"))) != 1 {
		t.Fatal("first round missing")
	}
	out := warmupOccurrences(c, at("2026-11-03T00:00:15+08:00"))
	if len(out) != 1 || !out[0].End.Equal(at("2026-11-03T01:00:00+08:00")) {
		t.Fatal("midnight round missing")
	}
	if len(warmupOccurrences(c, at("2026-11-03T00:02:00+08:00"))) != 0 {
		t.Fatal("missed round backfilled")
	}
	if len(warmupOccurrences(c, at("2026-11-03T23:00:00+08:00"))) != 0 {
		t.Fatal("wrong weekday")
	}
}
func TestWarmupOverridesAndTargetFilters(t *testing.T) {
	a := service.Account{ID: 1, Name: "fixture", GroupIDs: []int64{5}}
	rules := []warmupRule{{ID: 7, Enabled: true, Config: warmupConfig{GroupIDs: []int64{5}}}, {ID: 9, Enabled: true}}
	if r := effectiveWarmup(rules, a, warmupOverride{}); r == nil || r.ID != 7 {
		t.Fatal("target mismatch")
	}
	if r := effectiveWarmup(rules, a, warmupOverride{Mode: "disabled"}); r != nil {
		t.Fatal("excluded account matched")
	}
	id := int64(9)
	if r := effectiveWarmup(rules, a, warmupOverride{Mode: "rule", RuleID: &id}); r == nil || r.ID != 9 {
		t.Fatal("account override ignored")
	}
	id = 999
	if r := effectiveWarmup(rules, a, warmupOverride{Mode: "rule", RuleID: &id}); r != nil {
		t.Fatal("missing override fell back globally")
	}
}

func TestVaultWrappedJSONUnknownAndTrailingDataRejected(t *testing.T) {
	samples := []string{
		"{\"items\":[{\"email\":\"a@example.test\",\"unknown\":\"secret\"}]}",
		"[{\"email\":\"a@example.test\"}] [{\"email\":\"b@example.test\"}]",
	}
	for _, raw := range samples {
		rows := parseVaultImport(raw)
		if len(rows) != 1 || rows[0].Error == "" {
			t.Fatal("unknown or trailing import data silently dropped")
		}
	}
}
