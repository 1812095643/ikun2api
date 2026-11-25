package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/google/uuid"
)

var ErrCredentials = errors.New("通知凭据无法读取，请重新保存")

type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

func (s *Store) Channels(ctx context.Context) ([]domain.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.kind,c.name,c.enabled,c.settings,c.secret_fields,c.revision,c.updated_at,
        COALESCE(recent.state,''),COALESCE(recent.body->>'error_class',''),
        (SELECT max((o.body->>'accepted_at')::timestamptz) FROM ops_monitor_notification_outbox o WHERE o.channel=c.id AND o.state='accepted')
        FROM ops_monitor_channels c LEFT JOIN LATERAL(SELECT state,body FROM ops_monitor_notification_outbox WHERE channel=c.id ORDER BY next_attempt DESC,id DESC LIMIT 1) recent ON TRUE ORDER BY c.name,c.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Channel{}
	for rows.Next() {
		var c domain.Channel
		var settings, fields []byte
		if err = rows.Scan(&c.ID, &c.Kind, &c.Name, &c.Enabled, &settings, &fields, &c.Revision, &c.UpdatedAt, &c.LastState, &c.LastError, &c.LastAccepted); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(settings, &c.Settings); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(fields, &c.SecretConfigured); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Targets(ctx context.Context, enabledOnly bool) ([]domain.Target, error) {
	query := `SELECT t.id,t.channel_id,t.name,t.address,t.enabled,t.settings,c.kind FROM ops_monitor_targets t JOIN ops_monitor_channels c ON c.id=t.channel_id`
	if enabledOnly {
		query += ` WHERE t.enabled AND c.enabled`
	}
	query += ` ORDER BY c.name,t.name,t.id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Target{}
	for rows.Next() {
		var t domain.Target
		var settings []byte
		if err = rows.Scan(&t.ID, &t.ChannelID, &t.Name, &t.Address, &t.Enabled, &settings, &t.ChannelKind); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(settings, &t.Settings); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// 密钥只在实际投递时解密；常规列表只读取已配置字段名。
func (s *Store) ChannelSecret(ctx context.Context, id string, cipher Cipher) (domain.Channel, map[string]string, error) {
	var c domain.Channel
	var raw []byte
	var encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,name,enabled,settings,secret_ciphertext,revision FROM ops_monitor_channels WHERE id=$1`, id).Scan(&c.ID, &c.Kind, &c.Name, &c.Enabled, &raw, &encrypted, &c.Revision)
	if err != nil {
		return c, nil, err
	}
	if err = json.Unmarshal(raw, &c.Settings); err != nil {
		return c, nil, err
	}
	secret := map[string]string{}
	if encrypted != "" {
		if cipher == nil {
			return c, nil, ErrCredentials
		}
		plain, e := cipher.Decrypt(encrypted)
		if e != nil {
			return c, nil, ErrCredentials
		}
		if e = json.Unmarshal([]byte(plain), &secret); e != nil {
			return c, nil, ErrCredentials
		}
	}
	return c, secret, nil
}

func (s *Store) SaveChannel(ctx context.Context, input domain.ChannelInput, actor string, cipher Cipher) error {
	if err := input.Channel.Validate(); err != nil {
		return err
	}
	if len(input.Targets) > 50 || len(input.Secrets) > 4 || len(input.ClearSecrets) > 4 {
		return errors.New("渠道接收对象或凭据字段过多")
	}
	if actor == "" {
		return errors.New("请重新登录后保存")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, ConfigLock); err != nil {
		return err
	}
	var kind, encrypted string
	var version int
	err = tx.QueryRowContext(ctx, `SELECT kind,secret_ciphertext,revision FROM ops_monitor_channels WHERE id=$1 FOR UPDATE`, input.ID).Scan(&kind, &encrypted, &version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version != input.Revision {
		return ErrConflict
	}
	if kind != "" && kind != input.Kind {
		return errors.New("修改平台请新建通知渠道")
	}
	secrets := map[string]string{}
	if encrypted != "" {
		if cipher == nil {
			return errors.New("通知加密服务未就绪")
		}
		plain, e := cipher.Decrypt(encrypted)
		if e != nil {
			return errors.New("通知凭据无法解密，请重新保存")
		}
		if e = json.Unmarshal([]byte(plain), &secrets); e != nil {
			return e
		}
	}
	for _, key := range input.ClearSecrets {
		if !slices.Contains([]string{"webhook", "signing_secret", "app_secret", "bot_token"}, key) {
			return errors.New("请选择需要清除的凭据")
		}
		delete(secrets, key)
	}
	for key, value := range input.Secrets {
		if !slices.Contains([]string{"webhook", "signing_secret", "app_secret", "bot_token"}, key) || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("通知凭据格式有误")
		}
		if value != "" {
			secrets[key] = value
		}
	}
	if err = ValidateSecrets(input.Channel, secrets); err != nil {
		return err
	}
	fields := []string{}
	for key := range secrets {
		fields = append(fields, key)
	}
	slices.Sort(fields)
	if len(fields) > 0 {
		if cipher == nil {
			return errors.New("通知加密服务未就绪")
		}
		raw, e := json.Marshal(secrets)
		if e != nil {
			return e
		}
		encrypted, err = cipher.Encrypt(string(raw))
		if err != nil {
			return errors.New("通知凭据保存遇到问题")
		}
	} else {
		encrypted = ""
	}
	settings, _ := json.Marshal(input.Settings)
	if string(settings) == "null" {
		settings = []byte("{}")
	}
	fieldJSON, _ := json.Marshal(fields)
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_monitor_channels(id,kind,name,enabled,settings,secret_ciphertext,secret_fields,revision,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
	 ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,settings=excluded.settings,secret_ciphertext=excluded.secret_ciphertext,secret_fields=excluded.secret_fields,revision=excluded.revision,updated_at=NOW(),updated_by=excluded.updated_by`, input.ID, input.Kind, strings.TrimSpace(input.Name), input.Enabled, string(settings), encrypted, string(fieldJSON), version+1, actor)
	if err != nil {
		return err
	}
	ids, addresses := map[string]bool{}, map[string]bool{}
	for index := range input.Targets {
		t := &input.Targets[index]
		if t.ID == "" {
			t.ID = uuid.NewString()
		}
		t.ChannelID = input.ID
		if err = t.Validate(input.Kind); err != nil {
			return err
		}
		if ids[t.ID] || addresses[t.Address] {
			return errors.New("同一渠道的接收对象不能重复")
		}
		ids[t.ID] = true
		addresses[t.Address] = true
		var oldChannel, oldAddress string
		err = tx.QueryRowContext(ctx, `SELECT channel_id,address FROM ops_monitor_targets WHERE id=$1`, t.ID).Scan(&oldChannel, &oldAddress)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if oldChannel != "" && oldChannel != input.ID {
			return errors.New("接收对象属于其他渠道")
		}
		// 地址改变时使用新编号，旧队列不能投递给新接收人。
		if oldAddress != "" && oldAddress != t.Address {
			delete(ids, t.ID)
			t.ID = uuid.NewString()
			ids[t.ID] = true
		}
		raw, _ := json.Marshal(t.Settings)
		if string(raw) == "null" {
			raw = []byte("{}")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ops_monitor_targets(id,channel_id,name,address,settings,enabled) VALUES($1,$2,$3,$4,$5,$6)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name,settings=excluded.settings,enabled=excluded.enabled,updated_at=NOW()`, t.ID, t.ChannelID, t.Name, t.Address, string(raw), t.Enabled)
		if err != nil {
			return err
		}
	}
	kept := []string{}
	for id := range ids {
		kept = append(kept, id)
	}
	rawIDs, _ := json.Marshal(kept)
	if _, err = tx.ExecContext(ctx, `DELETE FROM ops_monitor_targets WHERE channel_id=$1 AND id NOT IN(SELECT jsonb_array_elements_text($2::jsonb))`, input.ID, string(rawIDs)); err != nil {
		return err
	}
	audit, _ := json.Marshal(map[string]any{"action": "channel.save", "channel_id": input.ID, "kind": input.Kind, "enabled": input.Enabled, "credential_changed": len(input.Secrets) > 0 || len(input.ClearSecrets) > 0, "targets": len(input.Targets)})
	if _, err = tx.ExecContext(ctx, `INSERT INTO ops_monitor_mutations(id,actor,body,created) VALUES($1,$2,$3,$4)`, uuid.NewString(), actor, string(audit), time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func ValidateSecrets(c domain.Channel, secrets map[string]string) error {
	if raw := secrets["webhook"]; raw != "" && !domain.ValidHTTPS(raw) {
		return errors.New("Webhook 需要不含用户名和密码的 HTTPS 地址")
	}
	if !c.Enabled {
		return nil
	}
	switch c.Kind {
	case "email":
	case "feishu_webhook", "lark_webhook", "wecom", "webhook":
		if secrets["webhook"] == "" {
			return errors.New("请先填写 Webhook 地址")
		}
	case "telegram":
		if secrets["bot_token"] == "" || strings.ContainsAny(secrets["bot_token"], "/?# ") {
			return errors.New("请先填写机器人 Token")
		}
	default:
		if c.Settings["app_id"] == "" || secrets["app_secret"] == "" {
			return errors.New("请先填写应用编号和密钥")
		}
		if c.Kind == "dingtalk" && c.Settings["robot_code"] == "" {
			return errors.New("请填写钉钉 Robot Code")
		}
	}
	return nil
}
