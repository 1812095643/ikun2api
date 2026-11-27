package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

func (s *Store) InsertTestDelivery(ctx context.Context, d domain.Delivery) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO ops_monitor_notification_outbox(id,incident_id,revision,channel,target,kind,state,next_attempt,lease_until,lease_token,attempts,body) VALUES($1,$2,$3,$4,$5,'test','leased',$6,$7,$8,1,$9)`, d.ID, d.IncidentID, d.Revision, d.Channel, d.Target, d.NextAttempt.UnixMilli(), d.LeaseUntil.UnixMilli(), d.LeaseToken, string(raw))
	return err
}

func (s *Store) ReturnLease(ctx context.Context, d domain.Delivery, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ops_monitor_notification_outbox SET state='pending',lease_until=0,attempts=GREATEST(attempts-1,0),next_attempt=$1 WHERE id=$2 AND lease_token=$3 AND state='leased'`, now.Add(2*time.Second).UnixMilli(), d.ID, d.LeaseToken)
	return err
}

// 同一机器人串行发送；独立渠道仍可在四个工作协程中并行。
func (s *Store) LockChannel(ctx context.Context, id string) (func(), bool, error) {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	slot := -1
	for candidate := 0; candidate < 4; candidate++ {
		var acquired bool
		if err = c.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(70932,$1)`, candidate).Scan(&acquired); err != nil {
			_ = c.Close()
			return nil, false, err
		}
		if acquired {
			slot = candidate
			break
		}
	}
	if slot < 0 {
		_ = c.Close()
		return nil, false, nil
	}
	unlockSlot := func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.ExecContext(release, `SELECT pg_advisory_unlock(70932,$1)`, slot)
	}
	var acquired bool
	if err = c.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(70931,hashtext($1))`, id).Scan(&acquired); err != nil || !acquired {
		unlockSlot()
		_ = c.Close()
		return nil, false, err
	}
	return func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.ExecContext(release, `SELECT pg_advisory_unlock(70931,hashtext($1))`, id)
		unlockSlot()
		_ = c.Close()
	}, true, nil
}

func (s *Store) LegacyAllAccepted(ctx context.Context, id string) (bool, error) {
	var total, accepted int
	err := s.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE state='accepted') FROM ops_monitor_notification_outbox WHERE incident_id=$1 AND kind='firing'`, id).Scan(&total, &accepted)
	return total > 0 && total == accepted, err
}

func (s *Store) WithEvaluationLock(ctx context.Context, run func() error) (bool, error) {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	var acquired bool
	if err = c.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, ConfigLock).Scan(&acquired); err != nil || !acquired {
		return false, err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.ExecContext(release, `SELECT pg_advisory_unlock($1)`, ConfigLock)
	}()
	return true, run()
}

func (s *Store) WithConfigLock(ctx context.Context, run func() error) error {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err = c.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, ConfigLock); err != nil {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.ExecContext(release, `SELECT pg_advisory_unlock($1)`, ConfigLock)
	}()
	return run()
}

func (s *Store) Target(ctx context.Context, id string) (domain.Target, error) {
	var t domain.Target
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT t.id,t.channel_id,t.name,t.address,t.enabled,t.settings,c.kind FROM ops_monitor_targets t JOIN ops_monitor_channels c ON c.id=t.channel_id WHERE t.id=$1`, id).Scan(&t.ID, &t.ChannelID, &t.Name, &t.Address, &t.Enabled, &raw, &t.ChannelKind)
	if err == nil {
		err = json.Unmarshal(raw, &t.Settings)
	}
	return t, err
}

func (s *Store) LegacyRecipient(ctx context.Context, address string) (domain.Target, error) {
	id := fmt.Sprintf("smtp-%x", sha256.Sum256([]byte(address)))
	_, err := s.db.ExecContext(ctx, `INSERT INTO ops_monitor_channels(id,kind,name,enabled) VALUES('system-smtp','email','系统邮件',true) ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		return domain.Target{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO ops_monitor_targets(id,channel_id,name,address) VALUES($1,'system-smtp',$2,$2) ON CONFLICT(id) DO NOTHING`, id, address)
	if err != nil {
		return domain.Target{}, err
	}
	return s.Target(ctx, id)
}

// 保留完整投递状态，列表无需读取每条任务附带的事件证据。
func (s *Store) DeliveryPage(ctx context.Context, incident, channel, state string, limit int) ([]domain.Delivery, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return readJSON[domain.Delivery](ctx, s.db, `SELECT (o.body || jsonb_build_object('state',o.state,'attempts',o.attempts,
	 'target_name',COALESCE(t.name,''),'channel_name',COALESCE(c.name,''),
	 'next_attempt',to_char(to_timestamp(o.next_attempt/1000.0) AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))) - 'payload'
	 FROM (SELECT * FROM ops_monitor_notification_outbox WHERE ($1='' OR incident_id=$1) AND ($2='' OR channel=$2) AND ($3='' OR state=$3)
	 ORDER BY next_attempt DESC,id DESC LIMIT $4) o
	 LEFT JOIN ops_monitor_targets t ON t.id=o.target LEFT JOIN ops_monitor_channels c ON c.id=o.channel
	 ORDER BY o.next_attempt DESC,o.id DESC`, incident, channel, state, limit)
}

func (s *Store) MarkDeliverySending(ctx context.Context, d domain.Delivery, now time.Time) error {
	r, err := s.db.ExecContext(ctx, `UPDATE ops_monitor_notification_outbox SET send_started_at=$1 WHERE id=$2 AND state='leased' AND lease_token=$3 AND lease_until>$1`, now.UnixMilli(), d.ID, d.LeaseToken)
	if err != nil {
		return err
	}
	count, err := r.RowsAffected()
	if err == nil && count != 1 {
		return fmt.Errorf("投递租约已变化")
	}
	return err
}
