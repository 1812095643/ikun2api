package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func testSEO(t *testing.T) (*seoData, seoSettings) {
	t.Helper()
	raw := `{"site":{"legal_name":"乘序科技","home_title":"模型 API 接入平台","description":"一个密钥调用主流模型。","keywords":"模型API,Claude API","summary":["兼容 OpenAI 与 Anthropic 协议"]},
	"docs":[{"id":"start","category":"网站入门","title":"从注册到首次使用","description":"按顺序完成首次使用。","body":"<section><h2 id=\"a\">第一步</h2><p>打开 <code>__IKUN_API_BASE__</code> 并登录 __IKUN_SITE_NAME__</p><svg><path d=\"x\"/></svg><script>alert(1)</script><a href=\"javascript:alert(1)\" onclick=\"x()\">坏链接</a><a href=\"/keys\" class=\"btn\">密钥</a></section>"},
	{"id":"claude","category":"工具接入","title":"Claude Code","description":"Anthropic 协议。","body":"<p>根地址不加 /v1</p>"}],
	"faqs":[{"question":"从哪里开始？","answer":"先创建密钥。<a href=\"#start\">快速开始</a>"}]}`
	d := parseSEOData([]byte(raw))
	if d == nil {
		t.Fatal("parse")
	}
	settings := `{"site_name":"心链词元","site_subtitle":"连接模型","api_base_url":"https://api.example.com/v1","home_content":"<style>.x{}</style><h1>连接模型</h1><p>为开发者提供模型 API</p><script>bad()</script>","login_agreement_documents":[{"id":"terms","title":"服务条款","content_md":"# 服务条款\n\n本服务由乘序科技运营。\n\n- 第一条\n- 第二条"}]}`
	return d, parseSEOSettings([]byte(settings))
}

const shell = `<!doctype html><html lang="zh-CN"><head><title>心链词元 - AI API Gateway</title></head><body><div id="app"></div><script type="module" src="/x.js"></script></body></html>`

func TestSEOHomePageHasMetadataStructuredDataAndText(t *testing.T) {
	d, s := testSEO(t)
	for _, path := range []string{"/", "/home"} {
		p, ok := d.page(path, s, "https://token.example.com")
		if !ok {
			t.Fatal(path)
		}
		out := string(renderSEO([]byte(shell), p, s.SiteName))
		for _, want := range []string{"<title>心链词元 - 模型 API 接入平台</title>", `<meta name="description" content="一个密钥调用主流模型。">`, `<link rel="canonical" href="https://token.example.com/">`, `"@type":"FAQPage"`, `"legalName":"乘序科技"`, `<div class="seo-static"><h1>连接模型</h1><p>为开发者提供模型 API</p>`, `og:title`} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: missing %s in\n%s", path, want, out)
			}
		}
		if strings.Contains(out, "bad()") || strings.Contains(out, ".x{}") || strings.Count(out, "<title>") != 1 {
			t.Fatal("home content must be sanitized and title replaced", out)
		}
	}
}

func TestSEOTutorialArticleIsSanitizedAndFilled(t *testing.T) {
	d, s := testSEO(t)
	p, ok := d.page("/tutorial/start", s, "https://token.example.com")
	if !ok {
		t.Fatal("article")
	}
	if p.Title != "从注册到首次使用 - 心链词元新手教程" || p.Canonical != "https://token.example.com/tutorial/start" {
		t.Fatal(p.Title, p.Canonical)
	}
	for _, bad := range []string{"<script", "<svg", "javascript:", "onclick", "class=", "__IKUN"} {
		if strings.Contains(p.Body, bad) {
			t.Fatalf("body contains %s: %s", bad, p.Body)
		}
	}
	for _, want := range []string{`<h2 id="a">第一步</h2>`, "<code>https://api.example.com</code>", "登录 心链词元", `<a href="/keys">密钥</a>`} {
		if !strings.Contains(p.Body, want) {
			t.Fatalf("missing %s in %s", want, p.Body)
		}
	}
	raw, _ := json.Marshal(p.JSONLD)
	if !strings.Contains(string(raw), `"TechArticle"`) || !strings.Contains(string(raw), `"BreadcrumbList"`) {
		t.Fatal(string(raw))
	}
	if _, ok = d.page("/tutorial/nope", s, "https://token.example.com"); ok {
		t.Fatal("unknown article must not be indexable")
	}
	idx, ok := d.page("/tutorial", s, "https://token.example.com")
	if !ok || !strings.Contains(idx.Body, `<a href="/tutorial/claude">Claude Code</a>`) {
		t.Fatal(idx.Body)
	}
}

func TestSEOLegalAndPrivateRoutes(t *testing.T) {
	d, s := testSEO(t)
	p, ok := d.page("/legal/terms", s, "https://token.example.com")
	if !ok || !strings.Contains(p.Body, "<ul><li>第一条</li><li>第二条</li></ul>") || p.Title != "服务条款 - 心链词元" {
		t.Fatal(p)
	}
	for _, path := range []string{"/dashboard", "/admin/users", "/login", "/keys", "/legal/unknown"} {
		if _, ok := d.page(path, s, "https://token.example.com"); ok {
			t.Fatal(path, "must not be indexed")
		}
	}
	out := string(renderNoIndex([]byte(shell)))
	if !strings.Contains(out, `<meta name="robots" content="noindex, nofollow"></head>`) {
		t.Fatal(out)
	}
}

func TestSEOFilesForHost(t *testing.T) {
	d, s := testSEO(t)
	o := "https://token.example.com"
	robots := d.robots(o)
	if !strings.Contains(robots, "Allow: /\n") || !strings.Contains(robots, "Disallow: /admin\n") || !strings.Contains(robots, "Sitemap: https://token.example.com/sitemap.xml") {
		t.Fatal(robots)
	}
	sm := d.sitemap(s, o)
	for _, want := range []string{"<loc>https://token.example.com/</loc>", "<loc>https://token.example.com/tutorial/start</loc>", "<loc>https://token.example.com/legal/terms</loc>"} {
		if !strings.Contains(sm, want) {
			t.Fatal(sm)
		}
	}
	short := d.llms(s, o, false)
	if !strings.HasPrefix(short, "# 心链词元\n\n> 一个密钥调用主流模型。") || !strings.Contains(short, "- [Claude Code](https://token.example.com/tutorial/claude)：Anthropic 协议。") || !strings.Contains(short, "API 根地址：https://api.example.com") || strings.Contains(short, "教程全文\n\n###") {
		t.Fatal(short)
	}
	full := d.llms(s, o, true)
	if !strings.Contains(full, "### 从注册到首次使用") || !strings.Contains(full, "#### 第一步") || strings.Contains(full, "alert") {
		t.Fatal(full)
	}
	if seoOrigin("evil.com/x") != "" || seoOrigin("token.example.com") != "https://token.example.com" {
		t.Fatal("origin")
	}
}
