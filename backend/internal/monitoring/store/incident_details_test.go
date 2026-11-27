package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/repository"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 存储层使用独立测试库，避免与运行时测试并发写入全局通知配置。
func TestIncidentDetailsPostgres(t *testing.T) {
	dsn := os.Getenv("OPS_MONITOR_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立的本地 OPS_MONITOR_STORE_TEST_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	s := store.New(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-48 * time.Hour)
	var group, account, actor int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id", "分组-"+uuid.NewString()).Scan(&group))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO accounts(name,platform,type,credentials) VALUES($1,'openai','apikey','{}') RETURNING id", "上游-"+uuid.NewString()).Scan(&account))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,username) VALUES($1,'test','值班管理员') RETURNING id", uuid.NewString()+"@example.test").Scan(&actor))
	i := domain.Incident{ID: uuid.NewString(), Fingerprint: uuid.NewString(), Rule: "U01", Status: "firing", Revision: 1, FirstSeen: old, LastSeen: old, LastFault: old, GroupID: group, Sample: &domain.IncidentSample{RequestID: "req-unknown", AccountID: account}}
	for _, request := range []string{"req-success", "req-failed", "req-cancelled", "req-unknown"} {
		i.Revision++
		require.NoError(t, s.SaveFinding(ctx, i, nil, uuid.NewString(), &domain.Finding{RequestID: request, AccountID: account}))
	}
	observations := []domain.Observation{}
	for _, outcome := range []string{"success", "failed", "cancelled"} {
		observations = append(observations, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: uuid.NewString(), Kind: "request.finished", OccurredAt: now, ObservedAt: now, RequestID: "req-" + outcome, Outcome: outcome, AccountID: account, GroupID: group, Quality: domain.Fresh, Metrics: map[string]float64{"elapsed_ms": 1500}})
	}
	// 尝试报错后仍成功，只有最终终态参与结果分布。
	observations = append(observations, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: uuid.NewString(), Kind: "attempt.failed", OccurredAt: now, RequestID: "req-success", UpstreamStatus: 500, Quality: domain.Fresh})
	require.NoError(t, s.Ingest(ctx, observations, domain.Cursor{Source: "gateway"}))
	completedID := uuid.NewString()
	completed := observations[0]
	completed.EventID = uuid.NewString()
	completed.RequestID = completedID
	require.NoError(t, s.Ingest(ctx, []domain.Observation{completed}, domain.Cursor{Source: "gateway"}))
	var stored int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM ops_monitor_request_outcomes WHERE request_id=$1", completedID).Scan(&stored))
	require.Zero(t, stored, "普通健康请求不新增长期终态存储")
	late := domain.Incident{ID: uuid.NewString(), Fingerprint: uuid.NewString(), Revision: 1, Status: "firing", FirstSeen: old, LastSeen: old}
	require.NoError(t, s.SaveFinding(ctx, late, nil, uuid.NewString(), &domain.Finding{RequestID: completedID}))
	lateCounts, err := s.IncidentContext(ctx, &late)
	require.NoError(t, err)
	require.Equal(t, 1, lateCounts.Success, "后发现的事件应按索引补齐已经完成的请求")
	counts, err := s.IncidentContext(ctx, &i)
	require.NoError(t, err)
	require.Equal(t, store.RequestOutcomeCounts{Success: 1, Failed: 1, Cancelled: 1, Unknown: 1}, counts)
	require.NotEmpty(t, i.GroupName)
	require.NotEmpty(t, i.SampleAccountName)
	first, err := s.IncidentRequests(ctx, i.ID, "", 2)
	require.NoError(t, err)
	require.True(t, first.HasMore)
	second, err := s.IncidentRequests(ctx, i.ID, first.AfterID, 2)
	require.NoError(t, err)
	require.Len(t, second.Items, 2)
	require.False(t, second.HasMore)
	require.NotEqual(t, first.Items[0].RequestID, second.Items[0].RequestID)
	accounts, err := s.IncidentAccounts(ctx, i.ID, "", 30)
	require.NoError(t, err)
	require.Len(t, accounts.Items, 1)
	require.Equal(t, account, accounts.Items[0].ID)
	require.False(t, accounts.Items[0].Deleted)
	// 名称变动每次直接解析当前对象，不会被前端长期缓存遮住。
	_, err = db.ExecContext(ctx, "UPDATE accounts SET name='已改名' WHERE id=$1", account)
	require.NoError(t, err)
	accounts, err = s.IncidentAccounts(ctx, i.ID, "", 30)
	require.NoError(t, err)
	require.Equal(t, "已改名", accounts.Items[0].Name)
	require.NoError(t, s.ControlIncident(ctx, i.ID, "ack", fmtInt(actor), uuid.NewString(), i.Revision, time.Time{}, "", now))
	current, err := s.Incident(ctx, i.ID)
	require.NoError(t, err)
	require.True(t, current.LastSeen.Equal(old), "人工操作不能伪造故障观测")
	require.True(t, current.LastControlAt.Equal(now))
	active, err := s.ListIncidents(ctx, store.IncidentFilter{Status: "active", Start: now.Add(-time.Hour), End: now, AccountID: account})
	require.NoError(t, err)
	require.Len(t, active.Items, 1, "未解决事件不受最近一小时限制")
	historical, err := s.ListIncidents(ctx, store.IncidentFilter{Start: now.Add(-time.Hour), End: now, GroupID: group})
	require.NoError(t, err)
	require.Empty(t, historical.Items, "确认操作不应把旧观测纳入最近一小时")
	history, err := s.IncidentHistory(ctx, i.ID, 0, 1, "operations")
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	require.Equal(t, "ack", history.Items[0].Action)
	require.Equal(t, "值班管理员", history.Items[0].ActorName)
	require.Equal(t, "firing", history.Items[0].PreviousStatus)
	require.NoError(t, s.ControlIncident(ctx, i.ID, "silence", fmtInt(actor), uuid.NewString(), current.Revision, now.Add(time.Hour), "维护", now))
	current, _ = s.Incident(ctx, i.ID)
	require.NoError(t, s.ControlIncident(ctx, i.ID, "close", fmtInt(actor), uuid.NewString(), current.Revision, time.Time{}, "人工关闭", now))
	for _, status := range []string{"acknowledged", "silenced"} {
		page, e := s.ListIncidents(ctx, store.IncidentFilter{Status: status, GroupID: group})
		require.NoError(t, e)
		require.Empty(t, page.Items, "终态不能混入%s筛选", status)
	}
	older, err := s.IncidentHistory(ctx, i.ID, history.BeforeID, 1, "operations")
	require.NoError(t, err)
	require.Empty(t, older.Items)
	channelID, targetID := uuid.NewString(), uuid.NewString()
	_, err = db.ExecContext(ctx, "INSERT INTO ops_monitor_channels(id,kind,name) VALUES($1,'email','值班邮件')", channelID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO ops_monitor_targets(id,channel_id,name,address) VALUES($1,$2,'值班管理员','review@example.test')", targetID, channelID)
	require.NoError(t, err)
	delivery := domain.Delivery{ID: uuid.NewString(), IncidentID: i.ID, Revision: i.Revision, Channel: channelID, Target: targetID, Kind: "firing", State: "accepted", AcceptedAt: now, NextAttempt: now}
	current, _ = s.Incident(ctx, i.ID)
	require.NoError(t, s.SaveIncident(ctx, *current, []domain.Delivery{delivery}))
	rows, err := s.DeliveryPage(ctx, i.ID, "", "", 30)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "值班邮件", rows[0].ChannelName)
	require.Equal(t, "值班管理员", rows[0].TargetName)
	require.True(t, rows[0].AcceptedAt.Equal(now))
}

func fmtInt(value int64) string { return fmt.Sprint(value) }
