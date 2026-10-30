package service

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const MaxClaudeTelemetryBodyBytes = 256 << 10

const (
	claudeTelemetryMaxEvents  = 200
	claudeTelemetryMaxEntries = 2048
	claudeTelemetryRetention  = 10 * time.Minute
	// 本机官方 Claude Code 2.1.301 的 1P 事件默认 scheduledDelayMillis 为 10000。
	claudeTelemetryInterval = 10 * time.Second
)

type ClaudeTelemetryBatch struct {
	Count    int
	triggers []claudeTelemetryTrigger
}

type claudeTelemetryTrigger struct {
	sessionID uuid.UUID
	eventID   uuid.UUID
	eventName string
}

// 只读取事件种类和本地关联所需的 UUID；所有其他字段，包括 base64 附加元数据，整块丢弃。
func ParseClaudeTelemetryBatch(body []byte) (*ClaudeTelemetryBatch, error) {
	invalid := fmt.Errorf("遥测格式不受支持")
	if len(body) > MaxClaudeTelemetryBodyBytes || !json.Valid(body) {
		return nil, invalid
	}
	root, err := filterClaudePrivacyObject(body, map[string]bool{"events": true}, nil)
	if err != nil {
		return nil, invalid
	}
	events := gjson.GetBytes(root, "events")
	if !events.IsArray() {
		return nil, invalid
	}
	items := events.Array()
	if len(items) > claudeTelemetryMaxEvents {
		return nil, invalid
	}
	batch := &ClaudeTelemetryBatch{Count: len(items)}
	for _, item := range items {
		event, err := filterClaudePrivacyObject([]byte(item.Raw), map[string]bool{"event_type": true, "event_data": true}, nil)
		if err != nil {
			return nil, invalid
		}
		if gjson.GetBytes(event, "event_type").String() != "ClaudeCodeInternalEvent" {
			continue
		}
		data, err := filterClaudePrivacyObject([]byte(gjson.GetBytes(event, "event_data").Raw), map[string]bool{"event_name": true, "session_id": true, "event_id": true}, nil)
		if err != nil {
			return nil, invalid
		}
		name := gjson.GetBytes(data, "event_name").String()
		if name != "tengu_api_success" && name != "tengu_api_error" {
			continue
		}
		session, eventID := gjson.GetBytes(data, "session_id"), gjson.GetBytes(data, "event_id")
		if session.Type != gjson.String || eventID.Type != gjson.String || !claudeTelemetryUUID(session.Str) || !claudeTelemetryUUID(eventID.Str) {
			continue
		}
		batch.triggers = append(batch.triggers, claudeTelemetryTrigger{sessionID: uuid.MustParse(session.Str), eventID: uuid.MustParse(eventID.Str), eventName: name})
	}
	return batch, nil
}

type claudeTelemetryObservationKey struct {
	apiKeyID  int64
	sessionID uuid.UUID
	eventName string
}

type claudeTelemetryEventKey struct {
	apiKeyID int64
	eventID  uuid.UUID
}

type claudeTelemetryObservation struct {
	delivery claudeTelemetryDelivery
	expires  time.Time
	readyAt  time.Time
}

// 有界内存只保存已重建的摘要；原始遥测、提示词、Git 信息不进入缓存或持久化。
// 同一 Key/会话/结果只保留最新一次；一份真实观测最多发送一次，不补造历史活动。
type claudeTelemetryBuffer struct {
	mu       sync.Mutex
	observed map[claudeTelemetryObservationKey]claudeTelemetryObservation
	seen     map[claudeTelemetryEventKey]time.Time
}

func (b *claudeTelemetryBuffer) prune(now time.Time) {
	for key, observation := range b.observed {
		if !now.Before(observation.expires) {
			delete(b.observed, key)
		}
	}
	for key, expires := range b.seen {
		if !now.Before(expires) {
			delete(b.seen, key)
		}
	}
}

func (b *claudeTelemetryBuffer) remember(apiKeyID int64, session string, delivery claudeTelemetryDelivery, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	if b.observed == nil {
		b.observed = make(map[claudeTelemetryObservationKey]claudeTelemetryObservation)
	}
	key := claudeTelemetryObservationKey{apiKeyID: apiKeyID, sessionID: uuid.MustParse(session), eventName: delivery.eventName}
	if _, exists := b.observed[key]; !exists && len(b.observed) >= claudeTelemetryMaxEntries {
		return
	}
	readyAt := now.Add(claudeTelemetryInterval)
	if previous, exists := b.observed[key]; exists {
		// 连续请求更新摘要时保留原定发送时间，避免活跃会话始终无法到期。
		readyAt = previous.readyAt
	}
	b.observed[key] = claudeTelemetryObservation{delivery: delivery, expires: now.Add(claudeTelemetryRetention), readyAt: readyAt}
}

// 定时任务与客户端触发共用同一把锁和同一份观测，一份摘要只能被领取一次。
func (b *claudeTelemetryBuffer) claimDue(now time.Time, limit int) []claudeTelemetryDelivery {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	var deliveries []claudeTelemetryDelivery
	for key, observation := range b.observed {
		if now.Before(observation.readyAt) {
			continue
		}
		merged := false
		for i := range deliveries {
			if appendClaudeTelemetryBatch(&deliveries[i], observation.delivery) {
				merged = true
				break
			}
		}
		if merged {
			delete(b.observed, key)
			continue
		}
		if len(deliveries) >= limit {
			continue
		}
		delete(b.observed, key)
		deliveries = append(deliveries, observation.delivery)
	}
	return deliveries
}

func (b *claudeTelemetryBuffer) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observed = nil
	b.seen = nil
}

// 按首份待发摘要的到期时间计时；空队列不启动定时器。
func (b *claudeTelemetryBuffer) nextDelay(now time.Time) (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	var earliest time.Time
	for _, observation := range b.observed {
		if earliest.IsZero() || observation.readyAt.Before(earliest) {
			earliest = observation.readyAt
		}
	}
	if earliest.IsZero() {
		return 0, false
	}
	if !earliest.After(now) {
		return 0, true
	}
	return earliest.Sub(now), true
}

func (b *claudeTelemetryBuffer) claim(apiKeyID int64, trigger claudeTelemetryTrigger, now time.Time) (claudeTelemetryDelivery, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	eventKey := claudeTelemetryEventKey{apiKeyID: apiKeyID, eventID: trigger.eventID}
	if _, seen := b.seen[eventKey]; seen || len(b.seen) >= claudeTelemetryMaxEntries {
		return claudeTelemetryDelivery{}, false
	}
	key := claudeTelemetryObservationKey{apiKeyID: apiKeyID, sessionID: trigger.sessionID, eventName: trigger.eventName}
	observation, exists := b.observed[key]
	if !exists {
		return claudeTelemetryDelivery{}, false
	}
	delete(b.observed, key)
	if b.seen == nil {
		b.seen = make(map[claudeTelemetryEventKey]time.Time)
	}
	b.seen[eventKey] = now.Add(claudeTelemetryRetention)
	return observation.delivery, true
}

// RelayClaudeTelemetry 不转发客户端事件；触发仅能领取同一已鉴权 Key 的实际出站摘要。
// 返回值只是进入本地有界发送队列的数量，供应商是否接收另按 accepted_count 确认。
func (s *GatewayService) RelayClaudeTelemetry(apiKeyID int64, batch *ClaudeTelemetryBatch) int {
	if s == nil || s.httpUpstream == nil || apiKeyID <= 0 || batch == nil || os.Getenv("SUB6_CLAUDE_TELEMETRY_ENABLED") != "true" {
		return 0
	}
	var deliveries []claudeTelemetryDelivery
	for _, trigger := range batch.triggers {
		if delivery, ok := s.claudeTelemetry.claim(apiKeyID, trigger, time.Now()); ok {
			deliveries = append(deliveries, delivery)
		}
	}
	queued := 0
	for _, delivery := range combineClaudeTelemetryDeliveries(deliveries) {
		if s.dispatchClaudeTelemetry(delivery) {
			queued += int(gjson.GetBytes(delivery.body, "events.#").Int())
		}
	}
	return queued
}
