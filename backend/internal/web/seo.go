package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// seoData is generated at build time (frontend/config/seoBundle.ts) from the
// tutorial articles and frontend/src/data/seo-site.json.
type seoData struct {
	Site struct {
		LegalName   string   `json:"legal_name"`
		HomeTitle   string   `json:"home_title"`
		Description string   `json:"description"`
		Keywords    string   `json:"keywords"`
		Summary     []string `json:"summary"`
	} `json:"site"`
	Docs []seoDoc `json:"docs"`
	FAQs []seoFAQ `json:"faqs"`
}

type seoDoc struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Minutes     int    `json:"minutes"`
}

type seoFAQ struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// seoSettings is the subset of the injected public settings SEO needs.
type seoSettings struct {
	SiteName    string `json:"site_name"`
	Subtitle    string `json:"site_subtitle"`
	APIBase     string `json:"api_base_url"`
	HomeContent string `json:"home_content"`
	Legal       []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content_md"`
	} `json:"login_agreement_documents"`
}

func parseSEOData(raw []byte) *seoData {
	var d seoData
	if len(raw) == 0 || json.Unmarshal(raw, &d) != nil {
		return nil
	}
	return &d
}

func parseSEOSettings(raw []byte) seoSettings {
	var s seoSettings
	_ = json.Unmarshal(raw, &s)
	if strings.TrimSpace(s.SiteName) == "" {
		s.SiteName = "AI API"
	}
	return s
}

type seoPage struct {
	Title       string
	Description string
	Keywords    string
	Canonical   string
	Type        string
	Body        string // sanitized HTML shown until the app mounts
	JSONLD      []any
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// seoOrigin is the public origin for canonical URLs; the site is HTTPS-only.
func seoOrigin(host string) string {
	if !hostPattern.MatchString(host) {
		return ""
	}
	return "https://" + strings.ToLower(host)
}

func (d *seoData) apiBase(s seoSettings, origin string) string {
	base := strings.TrimSpace(s.APIBase)
	if u, err := url.Parse(base); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		base = origin
	}
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
}

func (d *seoData) fill(body string, s seoSettings, origin string) string {
	return strings.NewReplacer("__IKUN_API_BASE__", d.apiBase(s, origin), "__IKUN_SITE_NAME__", s.SiteName).Replace(body)
}

func (d *seoData) doc(id string) *seoDoc {
	for n := range d.Docs {
		if d.Docs[n].ID == id {
			return &d.Docs[n]
		}
	}
	return nil
}

// page returns the crawlable description of a public path, or false for
// private app routes that should not be indexed.
func (d *seoData) page(path string, s seoSettings, origin string) (seoPage, bool) {
	org := map[string]any{"@type": "Organization", "name": s.SiteName, "url": origin + "/"}
	if d.Site.LegalName != "" {
		org["legalName"] = d.Site.LegalName
	}
	crumbs := func(items ...[2]string) any {
		list := []any{}
		for n, it := range items {
			list = append(list, map[string]any{"@type": "ListItem", "position": n + 1, "name": it[0], "item": origin + it[1]})
		}
		return map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": list}
	}
	switch {
	case path == "/" || path == "/home":
		title := s.SiteName
		if d.Site.HomeTitle != "" {
			title += " - " + d.Site.HomeTitle
		}
		body := sanitizeHTML(s.HomeContent, origin)
		if strings.TrimSpace(textOf(body)) == "" {
			body = d.homeFallback(s)
		}
		faq := []any{}
		for _, f := range d.FAQs {
			faq = append(faq, map[string]any{"@type": "Question", "name": f.Question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": textOf(d.fill(f.Answer, s, origin))}})
		}
		ld := []any{
			map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": s.SiteName, "alternateName": s.Subtitle, "url": origin + "/", "inLanguage": "zh-CN", "description": d.Site.Description, "publisher": org},
			map[string]any{"@context": "https://schema.org", "@type": "Service", "name": s.SiteName + " 模型 API", "serviceType": "大模型 API 接入", "provider": org, "areaServed": "CN", "url": origin + "/", "description": d.Site.Description},
		}
		if len(faq) > 0 {
			ld = append(ld, map[string]any{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": faq})
		}
		return seoPage{Title: title, Description: d.Site.Description, Keywords: d.Site.Keywords, Canonical: origin + "/", Type: "website", Body: body, JSONLD: ld}, true
	case path == "/tutorial" || path == "/tutorial/":
		var b strings.Builder
		fmt.Fprintf(&b, "<h1>%s 新手教程</h1><p>注册、充值、创建 API Key，以及在 Claude Code、Codex、Cursor、Cherry Studio 等工具中接入的图文说明。</p>", esc(s.SiteName))
		for _, cat := range d.categories() {
			fmt.Fprintf(&b, "<h2>%s</h2><ul>", esc(cat))
			for _, doc := range d.Docs {
				if doc.Category == cat {
					fmt.Fprintf(&b, `<li><a href="/tutorial/%s">%s</a>：%s</li>`, url.PathEscape(doc.ID), esc(doc.Title), esc(doc.Description))
				}
			}
			b.WriteString("</ul>")
		}
		return seoPage{Title: "新手教程与接入指南 - " + s.SiteName, Description: "从注册、充值、创建 API Key 到在 Claude Code、Codex、Cursor、Cherry Studio 等工具中接入，" + s.SiteName + " 的完整图文教程。", Canonical: origin + "/tutorial", Type: "website", Body: b.String(),
			JSONLD: []any{crumbs([2]string{s.SiteName, "/"}, [2]string{"新手教程", "/tutorial"})}}, true
	case strings.HasPrefix(path, "/tutorial/"):
		id, _ := url.PathUnescape(strings.TrimPrefix(path, "/tutorial/"))
		doc := d.doc(id)
		if doc == nil {
			return seoPage{}, false
		}
		body := fmt.Sprintf("<article><p><a href=\"/tutorial\">新手教程</a> / %s</p><h1>%s</h1><p>%s</p>%s</article>", esc(doc.Category), esc(doc.Title), esc(doc.Description), sanitizeHTML(d.fill(doc.Body, s, origin), origin))
		return seoPage{Title: doc.Title + " - " + s.SiteName + "新手教程", Description: doc.Description, Canonical: origin + "/tutorial/" + url.PathEscape(doc.ID), Type: "article", Body: body,
			JSONLD: []any{
				map[string]any{"@context": "https://schema.org", "@type": "TechArticle", "headline": doc.Title, "description": doc.Description, "inLanguage": "zh-CN", "articleSection": doc.Category, "author": org, "publisher": org, "mainEntityOfPage": origin + "/tutorial/" + url.PathEscape(doc.ID)},
				crumbs([2]string{s.SiteName, "/"}, [2]string{"新手教程", "/tutorial"}, [2]string{doc.Title, "/tutorial/" + url.PathEscape(doc.ID)}),
			}}, true
	case strings.HasPrefix(path, "/legal/"):
		id := strings.TrimPrefix(path, "/legal/")
		for _, l := range s.Legal {
			if l.ID == id && l.Title != "" {
				body := markdownToHTML(l.Content)
				return seoPage{Title: l.Title + " - " + s.SiteName, Description: firstSentence(textOf(body), 120), Canonical: origin + "/legal/" + url.PathEscape(id), Type: "article", Body: body}, true
			}
		}
	}
	return seoPage{}, false
}

func (d *seoData) categories() []string {
	seen, out := map[string]bool{}, []string{}
	for _, doc := range d.Docs {
		if !seen[doc.Category] {
			seen[doc.Category] = true
			out = append(out, doc.Category)
		}
	}
	return out
}

func (d *seoData) homeFallback(s seoSettings) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>%s</h1><p>%s</p><ul>", esc(s.SiteName), esc(d.Site.Description))
	for _, line := range d.Site.Summary {
		fmt.Fprintf(&b, "<li>%s</li>", esc(line))
	}
	b.WriteString(`</ul><p><a href="/tutorial">新手教程</a></p>`)
	return b.String()
}

func esc(s string) string { return htmlpkg.EscapeString(s) }

// render puts page metadata into <head> and the readable body inside #app;
// the Vue app replaces #app's content when it mounts.
func renderSEO(doc []byte, p seoPage, siteName string) []byte {
	head := &bytes.Buffer{}
	fmt.Fprintf(head, `<meta name="description" content="%s">`, esc(p.Description))
	if p.Keywords != "" {
		fmt.Fprintf(head, `<meta name="keywords" content="%s">`, esc(p.Keywords))
	}
	fmt.Fprintf(head, `<link rel="canonical" href="%s"><meta name="robots" content="index, follow, max-snippet:-1, max-image-preview:large">`, esc(p.Canonical))
	fmt.Fprintf(head, `<meta property="og:type" content="%s"><meta property="og:site_name" content="%s"><meta property="og:title" content="%s"><meta property="og:description" content="%s"><meta property="og:url" content="%s"><meta property="og:locale" content="zh_CN"><meta name="twitter:card" content="summary">`, p.Type, esc(siteName), esc(p.Title), esc(p.Description), esc(p.Canonical))
	head.WriteString(`<meta name="applicable-device" content="pc,mobile">`)
	for _, ld := range p.JSONLD {
		raw, err := json.Marshal(ld)
		if err != nil {
			continue
		}
		// "</" cannot appear inside the data block.
		fmt.Fprintf(head, `<script type="application/ld+json">%s</script>`, bytes.ReplaceAll(raw, []byte("</"), []byte(`<\/`)))
	}
	head.WriteString(`<style>#app>.seo-static{max-width:760px;margin:0 auto;padding:32px 20px;font:15px/1.75 system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;color:#334155}#app>.seo-static h1{font-size:26px;line-height:1.3;color:#0f172a}#app>.seo-static table{border-collapse:collapse}#app>.seo-static td,#app>.seo-static th{border:1px solid #e2e8f0;padding:4px 8px}#app>.seo-static pre{white-space:pre-wrap;background:#f8fafc;padding:12px}</style>`)
	doc = replaceTitle(doc, p.Title)
	doc = bytes.Replace(doc, []byte("</head>"), append(head.Bytes(), []byte("</head>")...), 1)
	return bytes.Replace(doc, []byte(`<div id="app"></div>`), []byte(`<div id="app"><div class="seo-static">`+p.Body+`</div></div>`), 1)
}

// renderNoIndex marks app-only routes (login, console, admin) as not for search.
func renderNoIndex(doc []byte) []byte {
	return bytes.Replace(doc, []byte("</head>"), []byte(`<meta name="robots" content="noindex, nofollow"></head>`), 1)
}

func replaceTitle(doc []byte, title string) []byte {
	start := bytes.Index(doc, []byte("<title>"))
	end := bytes.Index(doc, []byte("</title>"))
	if start < 0 || end < start {
		return doc
	}
	out := append([]byte{}, doc[:start]...)
	out = append(out, []byte("<title>"+esc(title)+"</title>")...)
	return append(out, doc[end+len("</title>"):]...)
}

var allowedTags = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "p": true, "a": true, "ul": true, "ol": true, "li": true, "section": true, "article": true, "strong": true, "em": true, "b": true, "code": true, "pre": true, "table": true, "thead": true, "tbody": true, "tr": true, "td": true, "th": true, "br": true, "blockquote": true, "dl": true, "dt": true, "dd": true, "span": true, "div": true, "kbd": true}
var droppedTags = map[string]bool{"script": true, "style": true, "svg": true, "noscript": true, "iframe": true, "template": true, "button": true, "form": true, "input": true, "select": true, "textarea": true, "nav": true, "header": true, "footer": true}

// sanitizeHTML keeps readable structure only: no scripts, styles, event
// handlers or classes; links limited to http(s), same-site paths and anchors.
func sanitizeHTML(in, origin string) string {
	z := html.NewTokenizer(strings.NewReader(in))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			if skip == 0 {
				b.WriteString(esc(string(z.Text())))
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if droppedTags[tag] {
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 || !allowedTags[tag] {
				continue
			}
			if tt == html.EndTagToken {
				b.WriteString("</" + tag + ">")
				continue
			}
			b.WriteString("<" + tag)
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				switch string(k) {
				case "href":
					if h := safeHref(string(v), origin); h != "" && tag == "a" {
						fmt.Fprintf(&b, ` href="%s"`, esc(h))
					}
				case "id":
					fmt.Fprintf(&b, ` id="%s"`, esc(string(v)))
				}
			}
			b.WriteString(">")
		}
	}
}

func safeHref(h, origin string) string {
	h = strings.TrimSpace(h)
	switch {
	case strings.HasPrefix(h, "#"), strings.HasPrefix(h, "/") && !strings.HasPrefix(h, "//"):
		return h
	case strings.HasPrefix(h, "https://"), strings.HasPrefix(h, "http://"):
		if _, err := url.Parse(h); err == nil {
			return h
		}
	}
	return ""
}

var spaces = regexp.MustCompile(`\s+`)

func textOf(in string) string {
	z := html.NewTokenizer(strings.NewReader(in))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(spaces.ReplaceAllString(b.String(), " "))
		case html.StartTagToken:
			if n, _ := z.TagName(); droppedTags[string(n)] {
				skip++
			} else {
				b.WriteString(" ")
			}
		case html.EndTagToken:
			if n, _ := z.TagName(); droppedTags[string(n)] && skip > 0 {
				skip--
			} else {
				b.WriteString(" ")
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

func firstSentence(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// markdownToHTML handles the headings, lists and paragraphs used in the legal documents.
func markdownToHTML(md string) string {
	var b strings.Builder
	inList := false
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		isItem := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")
		if inList && !isItem {
			b.WriteString("</ul>")
			inList = false
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "### "):
			b.WriteString("<h3>" + esc(line[4:]) + "</h3>")
		case strings.HasPrefix(line, "## "):
			b.WriteString("<h2>" + esc(line[3:]) + "</h2>")
		case strings.HasPrefix(line, "# "):
			b.WriteString("<h1>" + esc(line[2:]) + "</h1>")
		case isItem:
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString("<li>" + esc(line[2:]) + "</li>")
		default:
			b.WriteString("<p>" + esc(line) + "</p>")
		}
	}
	if inList {
		b.WriteString("</ul>")
	}
	return b.String()
}

// Paths never worth crawling. The app routes behind login also carry noindex.
var robotsDisallow = []string{"/api/", "/admin", "/dashboard", "/keys", "/usage", "/profile", "/orders", "/purchase", "/subscriptions", "/redeem", "/affiliate", "/pool", "/share/", "/key-usage", "/auth/", "/setup", "/reset-password", "/email-verify", "/ops-monitor-ui/", "/monitor", "/image-studio", "/tickets", "/batch-image", "/available-channels"}

func (d *seoData) robots(origin string) string {
	var b strings.Builder
	b.WriteString("User-agent: *\nAllow: /\n")
	for _, p := range robotsDisallow {
		b.WriteString("Disallow: " + p + "\n")
	}
	b.WriteString("\nSitemap: " + origin + "/sitemap.xml\n")
	return b.String()
}

func (d *seoData) sitemap(s seoSettings, origin string) string {
	urls := []string{"/", "/tutorial"}
	for _, doc := range d.Docs {
		urls = append(urls, "/tutorial/"+url.PathEscape(doc.ID))
	}
	for _, l := range s.Legal {
		if l.ID != "" {
			urls = append(urls, "/legal/"+url.PathEscape(l.ID))
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for n, u := range urls {
		priority := "0.6"
		switch {
		case n == 0:
			priority = "1.0"
		case n == 1:
			priority = "0.8"
		case strings.HasPrefix(u, "/legal/"):
			priority = "0.3"
		}
		fmt.Fprintf(&b, "  <url><loc>%s</loc><changefreq>weekly</changefreq><priority>%s</priority></url>\n", esc(origin+u), priority)
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// llms implements https://llmstxt.org: a short, factual overview for AI assistants.
func (d *seoData) llms(s seoSettings, origin string, full bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n> %s\n\n", s.SiteName, d.Site.Description)
	if d.Site.LegalName != "" {
		fmt.Fprintf(&b, "%s 由%s运营，网址 %s/ 。\n\n", s.SiteName, d.Site.LegalName, origin)
	}
	b.WriteString("## 关键信息\n\n")
	for _, line := range d.Site.Summary {
		b.WriteString("- " + line + "\n")
	}
	fmt.Fprintf(&b, "- API 根地址：%s（OpenAI 兼容：POST /v1/chat/completions、/v1/responses；Anthropic 兼容：POST /v1/messages）。\n", d.apiBase(s, origin))
	fmt.Fprintf(&b, "- 开始使用：注册账户 → 充值或兑换 → 在「API 密钥」创建 Key → 按教程配置客户端。\n\n")
	for _, cat := range d.categories() {
		fmt.Fprintf(&b, "## %s\n\n", cat)
		for _, doc := range d.Docs {
			if doc.Category != cat {
				continue
			}
			fmt.Fprintf(&b, "- [%s](%s/tutorial/%s)：%s\n", doc.Title, origin, url.PathEscape(doc.ID), doc.Description)
		}
		b.WriteString("\n")
	}
	if len(d.FAQs) > 0 {
		b.WriteString("## 常见问题\n\n")
		for _, f := range d.FAQs {
			fmt.Fprintf(&b, "**%s**\n%s\n\n", f.Question, textOf(d.fill(f.Answer, s, origin)))
		}
	}
	if full {
		b.WriteString("## 教程全文\n\n")
		for _, doc := range d.Docs {
			fmt.Fprintf(&b, "### %s\n\n来源：%s/tutorial/%s\n\n%s\n\n", doc.Title, origin, url.PathEscape(doc.ID), markdownText(d.fill(doc.Body, s, origin)))
		}
	} else {
		fmt.Fprintf(&b, "## 更多\n\n- [教程全文](%s/llms-full.txt)\n", origin)
	}
	return b.String()
}

// markdownText turns article HTML into plain markdown-ish text for llms-full.txt.
func markdownText(in string) string {
	z := html.NewTokenizer(strings.NewReader(in))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			out := regexp.MustCompile(`\n{3,}`).ReplaceAllString(b.String(), "\n\n")
			return strings.TrimSpace(out)
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			n, _ := z.TagName()
			tag := string(n)
			if droppedTags[tag] {
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 || tt == html.EndTagToken && tag != "p" && tag != "h2" && tag != "h3" && tag != "tr" && tag != "pre" {
				continue
			}
			switch tag {
			case "h2":
				if tt == html.StartTagToken {
					b.WriteString("\n\n#### ")
				} else {
					b.WriteString("\n")
				}
			case "h3":
				if tt == html.StartTagToken {
					b.WriteString("\n\n##### ")
				} else {
					b.WriteString("\n")
				}
			case "p", "pre", "tr":
				b.WriteString("\n")
			case "li":
				b.WriteString("\n- ")
			case "td", "th":
				b.WriteString(" | ")
			case "br":
				b.WriteString("\n")
			}
		case html.TextToken:
			if skip == 0 {
				b.WriteString(spaces.ReplaceAllString(string(z.Text()), " "))
			}
		}
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
