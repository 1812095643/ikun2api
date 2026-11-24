package notify

import (
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"html"
	"net/url"
	"strings"
	"time"
)

type object = map[string]any

func safeText(s string) string {
	s = html.EscapeString(s)
	for _, c := range []string{"*", "~", "[", "]", "(", ")", "#", "_", "`"} {
		s = strings.ReplaceAll(s, c, fmt.Sprintf("&#%d;", []rune(c)[0]))
	}
	return s
}
func detailLink(base, id string) string {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ops-monitor-ui/"
	q := u.Query()
	q.Set("monitor_incident", id)
	u.RawQuery = q.Encode()
	return u.String()
}
func statusText(s string) string {
	switch s {
	case "resolved":
		return "已恢复"
	case "recovering":
		return "恢复观察中"
	case "candidate":
		return "待确认"
	default:
		return "异常持续"
	}
}
func incidentStatusText(i domain.Incident) string {
	if i.Scope == "digest" {
		return "每日摘要"
	}
	if i.Status == "resolved" && i.ClosedBy == "quiet" {
		return domain.QuietClosedText(i.Rule)
	}
	if i.Rule == "L02" && i.Status == "resolved" {
		if i.Sample != nil && i.Sample.RequestID != "" && strings.HasSuffix(i.Scope, ":"+i.Sample.RequestID) {
			return "该请求等待已结束"
		}
		return "该范围出现较快请求，仍需观察"
	}
	return statusText(i.Status)
}
func timingDetails(i domain.Incident) string {
	if i.Rule != "L02" || i.Sample == nil {
		return ""
	}
	loc := time.FixedZone("CST", 8*3600)
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return "尚未记录"
		}
		return t.In(loc).Format("2006-01-02 15:04:05")
	}
	s := i.Sample
	text := "样本调用开始：" + stamp(s.RequestStartedAt) + "\n首次有效输出：" + stamp(s.FirstOutputAt) + "\n请求结束：" + stamp(s.RequestEndedAt)
	if !s.RequestStartedAt.IsZero() && !s.FirstOutputAt.IsZero() && !s.FirstOutputAt.Before(s.RequestStartedAt) {
		text += fmt.Sprintf("\n首个有效输出等待：%.1f秒", s.FirstOutputAt.Sub(s.RequestStartedAt).Seconds())
	}
	if !s.RequestEndedAt.IsZero() {
		switch s.RequestOutcome {
		case "success":
			text += "\n请求结果：成功完成"
		case "failed":
			text += "\n请求结果：失败"
		case "cancelled":
			text += "\n请求结果：已取消"
		default:
			text += "\n请求结果：未知"
		}
	}
	return text + "\n时间均为北京时间；请求等待结束不代表渠道整体恢复。"
}
func confidence(s string) string {
	switch s {
	case "high":
		return "高"
	case "medium":
		return "中"
	default:
		return "低"
	}
}
func bullets(xs []string) string {
	if len(xs) == 0 {
		return "无补充项"
	}
	if len(xs) > 6 {
		xs = xs[:6]
	}
	out := []string{}
	for _, x := range xs {
		r := []rune(x)
		if len(r) > 350 {
			x = string(r[:350]) + "…"
		}
		out = append(out, "• "+safeText(x))
	}
	return strings.Join(out, "\n")
}
func Card(i domain.Incident, base string) ([]byte, error) {
	color := "orange"
	if i.Severity <= "P1" {
		color = "red"
	}
	if i.Status == "resolved" && i.Scope != "digest" {
		color = "green"
	}
	md := func(text string) object { return object{"tag": "markdown", "content": text} }
	block := func(text string) object {
		return object{"tag": "column_set", "flex_mode": "none", "columns": []any{object{"tag": "column", "width": "weighted", "weight": 1, "elements": []any{md(text)}}}}
	}
	focusText := "**" + safeText(i.Explanation.Conclusion) + "**\n" + safeText(displayScope(i)) + fmt.Sprintf("\n受影响请求 %d · 用户 %d · 观测命中 %d", i.RequestCount, i.UserCount, i.Count)
	if timing := timingDetails(i); timing != "" {
		focusText += "\n" + safeText(timing)
	}
	focus := block(focusText)
	focus["background_style"] = color + "-50"
	evidence := block("**已确认事实**\n" + bullets(i.Explanation.Facts) + "\n**推断**\n" + bullets(i.Explanation.Inferences))
	advice := block("**建议处理**\n" + bullets(i.Explanation.Advice) + "\n**尚缺证据**\n" + bullets(i.Explanation.Missing))
	loc := time.FixedZone("CST", 8*3600)
	footer := fmt.Sprintf("事件 %s · 规则 %s v%d · 通知版本 %d\n告警触发 %s · 最近观测 %s（北京时间）\n原因置信度：%s；平台接收不等于人工已读。", i.ID, i.Rule, i.RuleVersion, i.Revision, i.FirstSeen.In(loc).Format("2006-01-02 15:04:05"), i.LastSeen.In(loc).Format("2006-01-02 15:04:05"), confidence(i.Explanation.Confidence))
	if link := detailLink(base, i.ID); link != "" {
		footer += "\n[查看受保护的事件详情](" + link + ")"
	}
	footer += "\n网关故障时详情可能暂不可用，请先按以上证据处理。"
	card := object{"schema": "2.0", "config": object{"update_multi": true, "width_mode": "default", "enable_forward": false}, "header": object{"title": object{"tag": "plain_text", "content": "心链词元异常通知 · " + i.Severity}, "subtitle": object{"tag": "plain_text", "content": incidentStatusText(i)}, "template": color}, "body": object{"direction": "vertical", "vertical_spacing": "12px", "elements": []any{focus, evidence, advice, object{"tag": "markdown", "text_size": "notation", "content": footer}}}}
	return json.Marshal(card)
}
func Plain(i domain.Incident, base string) string {
	return fmt.Sprintf("心链词元异常通知 %s %s\n%s\n范围：%s\n%s\n请求 %d / 用户 %d / 观测命中 %d\n事件：%s · 版本 %d\n事实：\n%s\n推断：\n%s\n缺失：\n%s\n建议：\n%s\n详情：%s\n平台接收不等于已读；网关故障时详情可能不可用。", i.Severity, incidentStatusText(i), i.Explanation.Conclusion, displayScope(i), timingDetails(i), i.RequestCount, i.UserCount, i.Count, i.ID, i.Revision, strings.Join(i.Explanation.Facts, "\n"), strings.Join(i.Explanation.Inferences, "\n"), strings.Join(i.Explanation.Missing, "\n"), strings.Join(i.Explanation.Advice, "\n"), detailLink(base, i.ID))
}
func displayScope(i domain.Incident) string {
	if i.DisplayScope != "" {
		return i.DisplayScope
	}
	return i.Scope
}
