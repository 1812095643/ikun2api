package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// The pre-optimization query is the reference for monetary and eligibility rules.
const legacyLotteryEvidenceQuery = `SELECT u.id,u.balance::text,u.created_at,
COALESCE((SELECT SUM(actual_cost) FROM usage_logs WHERE user_id=u.id AND created_at >= $2 AND created_at < $3),0)::text,
COALESCE((SELECT SUM(actual_cost) FROM usage_logs WHERE user_id=u.id),0)::text,
(COALESCE((SELECT SUM(value) FROM redeem_codes r WHERE r.used_by=u.id AND r.status='used' AND r.value>0
 AND r.type IN ('balance','admin_balance') AND r.credit_source IN ('manual','redeem') AND r.used_at >= $2 AND r.used_at < $3
 AND NOT EXISTS(SELECT 1 FROM payment_orders po WHERE po.recharge_code=r.code)),0)
 + COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id=u.id AND order_type='balance' AND status='COMPLETED' AND paid_at >= $2 AND paid_at < $3),0))::text,
(COALESCE((SELECT SUM(value) FROM redeem_codes r WHERE r.used_by=u.id AND r.status='used' AND r.value>0
 AND r.type IN ('balance','admin_balance') AND r.credit_source IN ('manual','redeem')
 AND NOT EXISTS(SELECT 1 FROM payment_orders po WHERE po.recharge_code=r.code)),0)
 + COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id=u.id AND order_type='balance' AND status='COMPLETED'),0))::text
FROM users u WHERE u.deleted_at IS NULL AND u.status='active'`

func TestLotteryEvidencePostgresMatchesExistingRules(t *testing.T) {
	dsn := os.Getenv("IKUN_LOTTERY_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable lottery PostgreSQL fixture required")
	}
	base, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	var database string
	if err = base.QueryRow("SELECT current_database()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != "lottery_fixture" {
		t.Fatal("dedicated lottery_fixture database required")
	}
	schema := "ikun_lottery_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE") })
	_, err = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,balance numeric NOT NULL,created_at timestamptz NOT NULL,status text,deleted_at timestamptz);
CREATE TABLE usage_logs(user_id bigint,created_at timestamptz,actual_cost numeric);
CREATE TABLE redeem_codes(used_by bigint,status text,value numeric,type text,credit_source text,used_at timestamptz,code text);
CREATE TABLE payment_orders(user_id bigint,order_type text,status text,amount numeric,paid_at timestamptz,recharge_code text);
CREATE TABLE workspace_lottery_entries(id bigserial PRIMARY KEY,lottery_id bigint,user_id bigint,evidence jsonb,signature text,joined_at timestamptz DEFAULT now());
INSERT INTO users VALUES(1,30,'2026-10-24T00:00:00Z','active',NULL),(2,0,'2026-10-25T00:00:00Z','active',NULL),(3,5,'2026-10-26T00:00:00Z','active',NULL),(4,10,'2026-10-24T00:00:00Z','disabled',NULL),(5,10,'2026-10-24T00:00:00Z','active',now()),(6,0,'2026-10-24T00:00:00Z','active',NULL);
INSERT INTO usage_logs VALUES(1,'2026-10-24T15:59:59.999999Z',7.00000001),(1,'2026-10-24T16:00:00Z',1.25),(1,'2026-10-25T15:59:59.999999Z',2.5),(1,'2026-10-25T16:00:00Z',8),(2,'2026-10-24T00:00:00Z',9.9),(2,'2026-10-25T00:00:00Z',0.1),(2,'2026-10-25T01:00:00Z',-0.1),(3,'2026-10-25T00:00:00Z',-0.25),(99,'2026-10-25T00:00:00Z',10000);
INSERT INTO redeem_codes VALUES(1,'used',5,'balance','redeem','2026-10-25T00:00:00Z','manual'),(1,'used',6,'admin_balance','manual','2026-10-24T00:00:00Z','previous'),(1,'used',21,'balance','redeem','2026-10-25T00:00:00Z','paid'),(1,'used',10,'admin_balance','gift','2026-10-25T00:00:00Z','gift'),(1,'used',12,'admin_balance','lottery','2026-10-25T00:00:00Z','lottery'),(1,'used',13,'balance','legacy','2026-10-25T00:00:00Z','legacy'),(1,'unused',14,'balance','redeem','2026-10-25T00:00:00Z','unused'),(1,'used',-2,'balance','redeem','2026-10-25T00:00:00Z','negative'),(1,'used',22,'balance','redeem','2026-10-25T00:00:00Z','pending');
INSERT INTO payment_orders VALUES(1,'balance','COMPLETED',21,'2026-10-25T00:00:00Z','paid'),(1,'balance','COMPLETED',3,'2026-10-25T16:00:00Z','at-end'),(1,'subscription','COMPLETED',500,'2026-10-25T00:00:00Z','subscription'),(1,'balance','PENDING',22,'2026-10-25T00:00:00Z','pending');
INSERT INTO workspace_lottery_entries(lottery_id,user_id) VALUES(10,1),(10,2),(11,3);`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	specs := [][]lotteryCondition{nil, {{Kind: "balance_min", Value: "0"}}, {{Kind: "registered_before", Value: "2026-10-25"}}, {{Kind: "user_ids", Value: "1,3"}}}
	for _, kind := range []string{"spend_today", "spend_total", "recharge_today", "recharge_total"} {
		for _, value := range []string{"0", "3.75", "26"} {
			specs = append(specs, []lotteryCondition{{Kind: kind, Value: value}})
		}
	}
	specs = append(specs, []lotteryCondition{{Kind: "spend_today", Value: "0"}, {Kind: "spend_total", Value: "10"}, {Kind: "recharge_today", Value: "20"}, {Kind: "recharge_total", Value: "30"}})
	for _, conditions := range specs {
		for _, match := range []string{"all", "any"} {
			spec := validLotterySpec()
			spec.Conditions = conditions
			spec.Match = match
			start, end := lotteryDateBounds(spec)
			for _, scope := range []lotteryEvidenceScope{lotteryEvidencePreview, lotteryEvidenceUser, lotteryEvidenceEntrants} {
				query := legacyLotteryEvidenceQuery
				args := []any{int64(1), start, end}
				switch scope {
				case lotteryEvidencePreview:
					query += " AND u.id >= $1 ORDER BY u.id LIMIT 100001"
				case lotteryEvidenceEntrants:
					query += ` AND u.id >= $1 AND EXISTS(SELECT 1 FROM workspace_lottery_entries e WHERE e.lottery_id=$4 AND e.user_id=u.id) ORDER BY u.id`
					args = append(args, int64(10))
				default:
					query += " AND u.id=$1"
				}
				read := func(query string) []lotteryEvidence {
					t.Helper()
					rows, e := db.QueryContext(ctx, query, args...)
					if e != nil {
						t.Fatal(e)
					}
					defer rows.Close()
					result := []lotteryEvidence{}
					for rows.Next() {
						v, e := scanLotteryEvidence(rows, spec)
						if e != nil {
							t.Fatal(e)
						}
						result = append(result, v)
					}
					if e = rows.Err(); e != nil {
						t.Fatal(e)
					}
					return result
				}
				want, got := read(query), read(lotteryEvidenceQuery(spec, scope))
				if len(want) != len(got) {
					t.Fatal("candidate scope changed")
				}
				for i := range want {
					if want[i].UserID != got[i].UserID || want[i].Balance != got[i].Balance || !want[i].RegisteredAt.Equal(got[i].RegisteredAt) {
						t.Fatal("candidate identity changed")
					}
					if lotteryMatches(spec, want[i]) != lotteryMatches(spec, got[i]) {
						t.Fatalf("eligibility changed for user %d with %#v", want[i].UserID, conditions)
					}
					expected := want[i]
					needed := map[string]bool{}
					for _, c := range conditions {
						needed[c.Kind] = true
					}
					if !needed["spend_today"] {
						expected.SpendToday = ""
					}
					if !needed["spend_total"] {
						expected.SpendTotal = ""
					}
					if !needed["recharge_today"] {
						expected.RechargeToday = ""
					}
					if !needed["recharge_total"] {
						expected.RechargeTotal = ""
					}
					// Numeric scale is presentation-only; normalize nonempty decimal facts.
					normalize := func(e lotteryEvidence) lotteryEvidence {
						for _, v := range []*string{&e.SpendToday, &e.SpendTotal, &e.RechargeToday, &e.RechargeTotal} {
							if *v != "" {
								d, err := decimal.NewFromString(*v)
								if err != nil {
									t.Fatal(err)
								}
								*v = d.String()
							}
						}
						return e
					}

					if !reflect.DeepEqual(normalize(expected), normalize(got[i])) {
						t.Fatalf("monetary fact changed: want %#v got %#v", expected, got[i])
					}
				}
			}
		}
	}
	// Fixed day bounds include the opening instant and exclude midnight of the next day.
	spec := validLotterySpec()
	spec.Conditions = []lotteryCondition{{Kind: "spend_today", Value: "3.75"}}
	evidence, eligible, err := (&Handler{db: db}).lotteryEligibility(ctx, db, spec, 1)
	if err != nil || eligible || evidence.SpendToday != "3.75" {
		t.Fatalf("strict threshold/day boundary changed: %#v %v", evidence, err)
	}
	count, _, err := (&Handler{db: db}).lotteryPreview(ctx, spec)
	if err != nil || count != 0 {
		t.Fatal("strict threshold preview changed")
	}
	// A real draw mixes pre-upgrade complete snapshots with new selective snapshots.
	_, err = db.Exec(`TRUNCATE workspace_lottery_entries;
 CREATE TABLE workspace_runtime(key text PRIMARY KEY,value jsonb,updated_at timestamptz DEFAULT now());
 CREATE TABLE workspace_lotteries(id bigserial PRIMARY KEY,spec jsonb,signature text,status text,created_by bigint,created_at timestamptz DEFAULT now(),published_at timestamptz,drawn_at timestamptz,eligible_count int DEFAULT 0,result_digest text DEFAULT '');
 CREATE TABLE workspace_lottery_events(id bigserial PRIMARY KEY,lottery_id bigint,actor_id bigint,action text,detail jsonb,created_at timestamptz DEFAULT now());
 CREATE TABLE workspace_lottery_winners(id bigserial PRIMARY KEY,lottery_id bigint,entry_id bigint,user_id bigint,prize_key text,amount numeric,signature text,created_at timestamptz DEFAULT now());
 INSERT INTO usage_logs VALUES(6,'2026-10-24T00:00:00Z',1);`)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{db: db, encryptor: lotteryFixtureEncryptor{}}
	key, err := h.lotteryKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec = validLotterySpec()
	spec.Conditions = []lotteryCondition{{Kind: "spend_total", Value: "0"}}
	spec.DrawMode = "full"
	spec.DrawAt = nil
	item := lotteryActivity{ID: 1, Spec: spec, CreatedBy: 1, Status: "open"}
	raw, _ := json.Marshal(spec)
	if _, err = db.Exec(`INSERT INTO workspace_lotteries(id,spec,signature,status,created_by,published_at) VALUES(1,$1,$2,'open',1,now())`, string(raw), lotterySpecSignature(key, item)); err != nil {
		t.Fatal(err)
	}
	start, end := lotteryDateBounds(spec)
	for i, uid := range []int64{1, 2, 6} {
		var evidence lotteryEvidence
		if i == 0 {
			evidence, err = scanLotteryEvidence(db.QueryRowContext(ctx, legacyLotteryEvidenceQuery+" AND u.id=$1", uid, start, end), spec)
		} else {
			evidence, _, err = h.lotteryEligibility(ctx, db, spec, uid)
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ = json.Marshal(evidence)
		if _, err = db.Exec(`INSERT INTO workspace_lottery_entries(lottery_id,user_id,evidence,signature) VALUES(1,$1,$2,$3)`, uid, string(raw), lotterySign(key, "entry", []any{int64(1), uid, evidence})); err != nil {
			t.Fatal(err)
		}
	}
	drawn, err := h.drawLottery(ctx, 1)
	if err != nil || !drawn {
		t.Fatalf("mixed-snapshot draw failed: %v", err)
	}
	var eligibleCount, winners, events int
	if err = db.QueryRow("SELECT eligible_count FROM workspace_lotteries WHERE id=1 AND status='drawn'").Scan(&eligibleCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM workspace_lottery_winners WHERE lottery_id=1").Scan(&winners); err != nil {
		t.Fatal(err)
	}
	drawn, err = h.drawLottery(ctx, 1)
	if err != nil || drawn {
		t.Fatal("draw must remain idempotent")
	}
	if err = db.QueryRow("SELECT count(*) FROM workspace_lottery_events WHERE lottery_id=1 AND action='draw'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if eligibleCount != 3 || winners != 2 || events != 1 {
		t.Fatal("draw eligibility, winner count or idempotency changed")
	}

}

// Plaintext is confined to this disposable test database.
type lotteryFixtureEncryptor struct{}

func (lotteryFixtureEncryptor) Encrypt(value string) (string, error) { return value, nil }
func (lotteryFixtureEncryptor) Decrypt(value string) (string, error) { return value, nil }
