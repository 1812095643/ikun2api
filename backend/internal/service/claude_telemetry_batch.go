package service

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/tidwall/gjson"
)

type claudeTelemetryPayload struct {
	Events []json.RawMessage `json:"events"`
}

func decodeClaudeTelemetryPayload(body []byte) (claudeTelemetryPayload, error) {
	var payload claudeTelemetryPayload
	if len(body) > MaxClaudeTelemetryBodyBytes {
		return payload, fmt.Errorf("遥测批次格式或大小不受支持")
	}
	if _, err := filterClaudePrivacyObject(body, nil, nil); err != nil ||
		json.Unmarshal(body, &payload) != nil || len(payload.Events) == 0 || len(payload.Events) > claudeTelemetryMaxEvents {
		return payload, fmt.Errorf("遥测批次格式或大小不受支持")
	}
	// Check every event again before merging or sending, including events after
	// the first one. Future callers must not bypass the ingress event allowlist.
	for _, event := range payload.Events {
		if _, err := filterClaudePrivacyObject(event, nil, nil); err != nil {
			return payload, fmt.Errorf("遥测批次事件格式不受支持")
		}
		data := []byte(gjson.GetBytes(event, "event_data").Raw)
		if _, err := filterClaudePrivacyObject(data, nil, nil); err != nil ||
			gjson.GetBytes(event, "event_type").String() != "ClaudeCodeInternalEvent" ||
			!isClaudeTelemetrySummaryEvent(gjson.GetBytes(data, "event_name").String()) {
			return payload, fmt.Errorf("遥测批次包含不允许发送的事件")
		}
	}
	return payload, nil
}

// 只合并相同本地身份、上游账号和出站配置；令牌或代理变化时必须分批。
func sameClaudeTelemetryOrigin(a, b claudeTelemetryDelivery) bool {
	return a.generation == b.generation && a.apiKeyID == b.apiKeyID && a.userID == b.userID && a.groupID == b.groupID &&
		a.accountID == b.accountID && a.authorization == b.authorization &&
		a.proxyURL == b.proxyURL && a.userAgent == b.userAgent && a.concurrency == b.concurrency &&
		reflect.DeepEqual(a.profile, b.profile) &&
		gjson.GetBytes(a.body, "events.0.event_data.auth.account_uuid").String() == gjson.GetBytes(b.body, "events.0.event_data.auth.account_uuid").String() &&
		gjson.GetBytes(a.body, "events.0.event_data.device_id").String() == gjson.GetBytes(b.body, "events.0.event_data.device_id").String()
}

// 每条 body 都来自服务端重建，禁止把原始客户端事件交给此函数。
func appendClaudeTelemetryBatch(batch *claudeTelemetryDelivery, next claudeTelemetryDelivery) bool {
	if !sameClaudeTelemetryOrigin(*batch, next) {
		return false
	}
	payload, err := decodeClaudeTelemetryPayload(batch.body)
	if err != nil {
		return false
	}
	additional, err := decodeClaudeTelemetryPayload(next.body)
	if err != nil || len(payload.Events)+len(additional.Events) > claudeTelemetryMaxEvents {
		return false
	}
	payload.Events = append(payload.Events, additional.Events...)
	body, err := json.Marshal(payload)
	if err != nil || len(body) > MaxClaudeTelemetryBodyBytes {
		return false
	}
	batch.body = body
	if batch.eventName != next.eventName {
		batch.eventName = "batch"
	}
	return true
}

func combineClaudeTelemetryDeliveries(deliveries []claudeTelemetryDelivery) []claudeTelemetryDelivery {
	batches := make([]claudeTelemetryDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		merged := false
		for i := range batches {
			if appendClaudeTelemetryBatch(&batches[i], delivery) {
				merged = true
				break
			}
		}
		if !merged {
			batches = append(batches, delivery)
		}
	}
	return batches
}
