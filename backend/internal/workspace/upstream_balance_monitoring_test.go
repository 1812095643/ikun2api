package workspace

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/stretchr/testify/require"
)

func TestUpstreamBalanceMinimumHandlesSmallDifferencesAndUnlimited(t *testing.T) {
	reading := func(value float64) upstreamBalanceReading {
		return upstreamBalanceReading{Scope: "wallet", Unit: "USD", Balance: &value}
	}
	for _, tc := range []struct {
		name     string
		readings []upstreamBalanceReading
		want     float64
	}{
		{"几毫差异也取最小值", []upstreamBalanceReading{reading(1937.5971), reading(1937.5814), reading(1937.5906)}, 1937.5814},
		{"失败读数不参与", []upstreamBalanceReading{{Error: "不可用"}, reading(12)}, 12},
		{"负余额参与比较", []upstreamBalanceReading{reading(2), reading(-1)}, -1},
		{"无限额度不覆盖有限余额", []upstreamBalanceReading{{Scope: "wallet", Unit: "USD", Unlimited: true}, reading(0)}, 0},
		{"非有限值不参与", []upstreamBalanceReading{reading(math.NaN()), reading(5)}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := summarizeUpstreamBalances(tc.readings, 0)
			require.NotNil(t, s.Balance)
			require.Equal(t, tc.want, *s.Balance)
			require.Empty(t, s.Error)
		})
	}
	a, b := reading(1), reading(7)
	b.Unit = "CNY"
	s := summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 0)
	require.Nil(t, s.Balance)
	require.NotEmpty(t, s.Error)
}

func TestUpstreamBalanceMonitoringPostgresBulkPreservesSourcesAndRollsBack(t *testing.T) {
	h, router := toolsPostgresFixture(t)
	_, err := h.db.Exec(`ALTER TABLE accounts ADD COLUMN proxy_id bigint;
	INSERT INTO accounts(id,name,platform,type,credentials) VALUES
	 (5,'A','openai','apikey','{"base_url":"https://balance-a.example","api_key":"test-only-a"}'),
	 (6,'B','anthropic','apikey','{"base_url":"https://balance-b.example","api_key":"test-only-b"}');`)
	require.NoError(t, err)
	for _, name := range []string{"260_workspace_upstream_balances.sql", "262_workspace_upstream_models.sql"} {
		raw, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = h.db.Exec(string(raw))
		require.NoError(t, err)
	}
	_, records, err := h.prepareUpstreamBalances(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 2)
	_, err = h.db.Exec(`UPDATE workspace_upstream_balances SET secret_blob='encrypted-test-only',config=config || '{"preferred_account_id":5,"threshold":99}'::jsonb WHERE id=$1`, records[0].ID)
	require.NoError(t, err)
	router.PUT("/monitoring", h.saveUpstreamBalanceMonitoring)
	payload := map[string]any{"all": true, "monitor_enabled": true, "interval_minutes": 15, "threshold": 10, "recipients": []string{"ADMIN@example.test", "admin@example.test"}}
	status, reply := toolsRequest(t, router, "PUT", "/monitoring", payload)
	require.Equal(t, 200, status, reply)
	require.Equal(t, float64(2), reply["data"].(map[string]any)["affected"])
	var count int
	require.NoError(t, h.db.QueryRow(`SELECT COUNT(*) FROM workspace_upstream_balances WHERE config @> '{"monitor_enabled":true,"interval_minutes":15,"threshold":10,"recipients":["admin@example.test"]}'::jsonb`).Scan(&count))
	require.Equal(t, 2, count)
	var secret string
	var preferred int64
	require.NoError(t, h.db.QueryRow(`SELECT secret_blob,(config->>'preferred_account_id')::bigint FROM workspace_upstream_balances WHERE id=$1`, records[0].ID).Scan(&secret, &preferred))
	require.Equal(t, "encrypted-test-only", secret)
	require.Equal(t, int64(5), preferred)
	// 重试相同配置不清空已发送记录，防止持续低余额时重复通知。
	_, err = h.db.Exec(`UPDATE workspace_upstream_balances SET alert='{"low":true,"recipients_sent":["admin@example.test"]}'`)
	require.NoError(t, err)
	status, reply = toolsRequest(t, router, "PUT", "/monitoring", payload)
	require.Equal(t, 200, status, reply)
	require.Equal(t, float64(0), reply["data"].(map[string]any)["affected"])
	require.NoError(t, h.db.QueryRow(`SELECT COUNT(*) FROM workspace_upstream_balances WHERE alert->>'low'='true'`).Scan(&count))
	require.Equal(t, 2, count)
	delete(payload, "all")
	payload["ids"] = []string{records[0].ID}
	payload["threshold"] = 3
	status, reply = toolsRequest(t, router, "PUT", "/monitoring", payload)
	require.Equal(t, 200, status, reply)
	require.Equal(t, float64(1), reply["data"].(map[string]any)["affected"])
	var stored upstreamBalanceMonitoring
	require.NoError(t, h.getRuntime(context.Background(), "upstream_balance_monitoring", &stored))
	require.Equal(t, float64(3), stored.Threshold)
	_, err = h.db.Exec(`CREATE FUNCTION reject_monitor_defaults() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'test-only rollback'; END $$ LANGUAGE plpgsql;
	CREATE TRIGGER reject_monitor_defaults BEFORE INSERT OR UPDATE ON workspace_runtime FOR EACH ROW EXECUTE FUNCTION reject_monitor_defaults()`)
	require.NoError(t, err)
	payload["threshold"] = 1
	status, _ = toolsRequest(t, router, "PUT", "/monitoring", payload)
	require.Equal(t, 500, status)
	var raw []byte
	require.NoError(t, h.db.QueryRow(`SELECT config FROM workspace_upstream_balances WHERE id=$1`, records[0].ID).Scan(&raw))
	var cfg upstreamBalanceConfig
	require.NoError(t, json.Unmarshal(raw, &cfg))
	require.Equal(t, float64(3), cfg.Threshold)
}
