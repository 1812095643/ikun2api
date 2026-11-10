package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLotteryPublishedEditPostgresAtomicAndDraw(t *testing.T) {
	dsn := os.Getenv("IKUN_LOTTERY_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable lottery PostgreSQL fixture required")
	}
	base, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	var name string
	if err = base.QueryRow("SELECT current_database()").Scan(&name); err != nil || name != "lottery_fixture" {
		t.Fatal("dedicated lottery_fixture database required", err)
	}
	schema := "ikun_edit_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE") })
	_, err = db.Exec(`CREATE TABLE workspace_runtime(key text PRIMARY KEY,value jsonb,updated_at timestamptz DEFAULT now());
 CREATE TABLE workspace_lotteries(id bigserial PRIMARY KEY,spec jsonb,signature text,status text DEFAULT 'draft',created_by bigint,created_at timestamptz DEFAULT now(),published_at timestamptz,drawn_at timestamptz,eligible_count int DEFAULT 0,result_digest text DEFAULT '');
 CREATE TABLE workspace_lottery_entries(id bigserial PRIMARY KEY,lottery_id bigint,user_id bigint,evidence jsonb,signature text,joined_at timestamptz DEFAULT now());
 CREATE TABLE workspace_lottery_events(id bigserial PRIMARY KEY,lottery_id bigint,actor_id bigint,action text,detail jsonb,created_at timestamptz DEFAULT now());
 CREATE TABLE workspace_lottery_winners(id bigserial PRIMARY KEY,lottery_id bigint,entry_id bigint,user_id bigint,prize_key text,amount numeric,signature text,created_at timestamptz DEFAULT now());
 CREATE TABLE users(id bigint PRIMARY KEY,balance numeric,created_at timestamptz,status text,deleted_at timestamptz);
 CREATE TABLE usage_logs(user_id bigint,created_at timestamptz,actual_cost numeric);
 CREATE TABLE redeem_codes(used_by bigint,status text,value numeric,type text,credit_source text,used_at timestamptz,code text);
 CREATE TABLE payment_orders(user_id bigint,order_type text,status text,amount numeric,paid_at timestamptz,recharge_code text);
 INSERT INTO users VALUES(8,10,now()-interval '1 day','active',NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h := &Handler{db: db, encryptor: lotteryFixtureEncryptor{}}
	original := futureEditableLottery()
	original.Spec.Conditions = []lotteryCondition{}
	created, err := h.saveLotteryActivity(ctx, actor{ID: 1, Admin: true}, 0, original.Spec, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE workspace_lotteries SET status='open',published_at=now() WHERE id=$1", created.ID); err != nil {
		t.Fatal(err)
	}
	key, err := h.lotteryKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old, err := h.loadLottery(ctx, db, created.ID, false, key)
	if err != nil {
		t.Fatal(err)
	}
	evidence := lotteryEvidence{UserID: 8, BusinessDate: old.Spec.BusinessDate}
	raw, _ := json.Marshal(evidence)
	if _, err = db.Exec("INSERT INTO workspace_lottery_entries(lottery_id,user_id,evidence,signature) VALUES($1,8,$2,$3)", old.ID, string(raw), lotterySign(key, "entry", []any{old.ID, int64(8), evidence})); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = db.QueryRow("SELECT md5(row_to_json(e)::text) FROM workspace_lottery_entries e").Scan(&before); err != nil {
		t.Fatal(err)
	}
	next := copyLotterySpec(old.Spec)
	deadline := time.Now().Add(time.Hour)
	next.DrawMode = "either"
	next.DrawAt = &deadline
	updated, err := h.saveLotteryActivity(ctx, actor{ID: 1, Admin: true}, old.ID, next, lotteryRevision(old))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "open" || updated.Spec.DrawMode != "either" {
		t.Fatal("published state lost")
	}
	// Two admins editing the same revision cannot overwrite each other.
	var group sync.WaitGroup
	result := make(chan error, 2)
	for _, description := range []string{"First admin", "Second admin"} {
		group.Add(1)
		go func(description string) {
			defer group.Done()
			edit := copyLotterySpec(updated.Spec)
			edit.Description = description
			_, err := h.saveLotteryActivity(ctx, actor{ID: 1, Admin: true}, updated.ID, edit, lotteryRevision(updated))
			result <- err
		}(description)
	}
	group.Wait()
	close(result)
	success, conflicts := 0, 0
	for err := range result {
		if err == nil {
			success++
		} else {
			var conflict *requestError
			if errors.As(err, &conflict) && conflict.status == 409 {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("lost update: success=%d conflicts=%d", success, conflicts)
	}
	var after string
	if err = db.QueryRow("SELECT md5(row_to_json(e)::text) FROM workspace_lottery_entries e").Scan(&after); err != nil || before != after {
		t.Fatal("existing entrant changed", err)
	}
	current, err := h.loadLottery(ctx, db, old.ID, false, key)
	if err != nil {
		t.Fatal("updated rules signature failed", err)
	}
	timed := copyLotterySpec(current.Spec)
	soon := time.Now().Add(300 * time.Millisecond)
	timed.DrawAt = &soon
	if _, err = h.saveLotteryActivity(ctx, actor{ID: 1, Admin: true}, old.ID, timed, lotteryRevision(current)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	drawn, err := h.drawLottery(ctx, old.ID)
	if err != nil || !drawn {
		t.Fatal("edited timed draw failed", err)
	}
	again, err := h.drawLottery(ctx, old.ID)
	if err != nil || again {
		t.Fatal("edited draw repeated", err)
	}
	var wins, events int
	if err = db.QueryRow("SELECT count(*) FROM workspace_lottery_winners").Scan(&wins); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM workspace_lottery_events WHERE action='draw'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if wins != 1 || events != 1 {
		t.Fatalf("draw duplicated/missed: wins=%d events=%d", wins, events)
	}
}
