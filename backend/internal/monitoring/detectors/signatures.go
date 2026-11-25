package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"time"
)

func (e *evaluation) signature(o domain.Observation) bool {
	s, ok := e.signatures[signatureRequestKey(o)]
	if !ok {
		return false
	}
	if o.EventID != s.EventID || o.Source != s.Source {
		if (o.Kind == "request.finished" && o.ClientStatus == 400) || o.Kind == "signature.rejected" {
			return true
		}
		return (o.Kind == "request.error" || o.Kind == "attempt.failed") && o.UpstreamStatus == 400
	}
	if strings.HasPrefix(o.Source, "postgres.") && o.Quality != domain.Historical && e.now.Sub(o.ObservedAt) < 30*time.Second {
		return true
	}
	scope := fmt.Sprintf("signature:unknown:user:%d/key:%d/group:%d/model:%s", o.UserID, o.KeyID, o.GroupID, o.Model)
	facts := []string{"上游明确拒绝一次请求尝试的历史thinking块签名，不是正常思考等待"}
	missing := []string{"没有历史签名原文，不做密码学验签；不能仅凭400判定客户端或渠道损坏"}
	if strings.HasPrefix(o.TaskID, "session:") && o.N("signature_session_known") == 1 {
		scope = "signature:" + o.TaskID
		facts = append(facts, "按鉴权用户、Key、分组和显式会话哈希合并；同会话换账号不新建通知")
	} else {
		facts = append(facts, "会话标识缺失：仅按用户/Key/分组/模型聚合，不能确认是同一会话")
	}
	if o.Has("signature_message_index") {
		facts = append(facts, fmt.Sprintf("上游指出messages.%.0f.content.%.0f的历史块", o.N("signature_message_index"), o.N("signature_block_index")))
	}
	conclusion := "会话历史思考签名无效（损坏环节未确认）"
	switch {
	case o.N("signature_changed_in_forward") == 1:
		conclusion = "签名关联内容在网关转发阶段发生变化"
		facts = append(facts, "同一签名对应的thinking内容在输入与发送间不同，需检查网关改写")
	case o.N("signature_matches_previous") == 1 && o.N("signature_forward_unchanged") == 1:
		conclusion = "上游拒绝了此前返回且原样回传的思考块"
		facts = append(facts, "块匹配同账号/模型/会话近期上游响应摘要，且本次输入与发送摘要一致；优先查上游校验环境或路由差异")
	case o.N("signature_changed_before_forward") == 1:
		conclusion = "思考块在进入本次转发处理前已发生变化"
		facts = append(facts, "签名匹配近期响应但thinking内容不同；需检查客户端存储、前置链路及网关转发前处理，不能直接认定客户端责任")
	case o.N("signature_forward_unchanged") == 1:
		facts = append(facts, "本次进入转发函数与发往上游的出错块摘要一致；更早来源未确认")
	}
	severity := "P1"
	if end, ok := e.ends[requestKey(o)]; ok && end.Outcome == "success" {
		severity = "P2"
		facts = append(facts, "本请求后续已有成功终态；不代表历史会话内容已修复")
	}
	f := e.add("F02", o, scope, "thinking_signature_invalid", severity, false, facts, missing)
	if f != nil {
		f.Explanation.Conclusion = conclusion
		f.Explanation.Confidence = "medium"
		f.Explanation.Advice = []string{"不要原样无限重试；保留任务摘要，新会话不携带损坏的thinking历史可作为临时恢复方案", "核对客户端持久化、上游返回与再次回传的完整性；不自动删除工具历史、不伪造signature；同范围持续问题最多每小时更新"}
	}
	return true
}

func signatureRequestKey(o domain.Observation) string {
	if o.RequestID == "" {
		return o.Site + ":event:" + o.Source + ":" + o.EventID
	}
	return requestKey(o)
}
