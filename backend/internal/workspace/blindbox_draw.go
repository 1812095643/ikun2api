package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/shopspring/decimal"
)

func shuffleBlindbox(rewards []int64) error {
	for i := len(rewards) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		rewards[i], rewards[j.Int64()] = rewards[j.Int64()], rewards[i]
	}
	return nil
}

func (h *Handler) settleBlindbox(ctx context.Context, id int64) (bool, error) {
	key, err := h.lotteryKey(ctx)
	if err != nil {
		return false, err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	r, err := loadBlindboxRound(ctx, tx, id, "", key, true)
	if err != nil {
		return false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return false, err
	}
	if r.Status != "open" || now.Before(r.DrawAt) {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+` WHERE e.round_id=$1 ORDER BY e.user_id,e.id FOR UPDATE OF e`, id)
	if err != nil {
		return false, err
	}
	entries := []blindboxEntry{}
	for rows.Next() {
		e, readErr := scanBlindboxEntry(rows)
		if readErr == nil {
			readErr = verifyBlindboxEntry(e, key)
		}
		if readErr != nil {
			rows.Close()
			return false, readErr
		}
		if e.Paid != r.Spec.Price || e.Reward != nil {
			rows.Close()
			return false, fmt.Errorf("盲盒购买金额或结算状态不一致")
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	plan, err := makeBlindboxPlan(len(entries), r.Spec)
	if err != nil {
		return false, err
	}
	paid, err := decimal.NewFromString(r.TotalPaid)
	if err != nil {
		return false, err
	}
	if r.Count != len(entries) || !paid.Equal(decimal.RequireFromString(plan.Total)) {
		return false, fmt.Errorf("盲盒购买总额与奖池不一致")
	}
	// 校验真实扣款流水，数据库内单独修改报名、金额或流水关联都不能被直接用于发奖。
	var inconsistent int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_blindbox_entries e LEFT JOIN redeem_codes d ON d.id=e.debit_id
        WHERE e.round_id=$1 AND (d.id IS NULL OR d.used_by IS DISTINCT FROM e.user_id OR d.value IS DISTINCT FROM -e.paid OR d.status<>'used' OR d.type<>'admin_balance' OR d.credit_source<>'gift')`, id).Scan(&inconsistent); err != nil {
		return false, err
	}
	if inconsistent != 0 {
		return false, fmt.Errorf("盲盒扣款流水校验未通过")
	}
	if err = shuffleBlindbox(plan.Rewards); err != nil {
		return false, err
	}
	credited := map[int64]bool{}
	results := make([]any, 0, len(entries))
	for i, e := range entries {
		amount := blindboxMoney(plan.Rewards[i])
		// 用户和明细按相同顺序加锁，多场补开奖也不会以相反顺序锁定余额。
		creditID, _, creditErr := service.CreditCommittedBalanceTx(ctx, tx, e.UserID, amount, "BWIN-"+strconv.FormatInt(e.ID, 36), "[盲盒奖励] "+r.Date)
		if creditErr != nil {
			return false, creditErr
		}
		signature := blindboxRewardSign(key, e, amount, creditID)
		result, writeErr := tx.ExecContext(ctx, `UPDATE workspace_blindbox_entries SET reward=$2::numeric,credit_id=$3,reward_signature=$4 WHERE id=$1 AND reward IS NULL`, e.ID, amount, creditID, signature)
		if writeErr != nil {
			return false, writeErr
		}
		if count, eErr := result.RowsAffected(); eErr != nil || count != 1 {
			return false, fmt.Errorf("盲盒奖励记录未完整保存")
		}
		credited[e.UserID] = true
		results = append(results, []any{e.ID, e.UserID, amount, creditID, signature})
	}
	raw, _ := json.Marshal(results)
	digest := sha256.Sum256(raw)
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_blindbox_rounds SET status='settled',total_awarded=$2::numeric,result_digest=$3,settled_at=clock_timestamp() WHERE id=$1`, id, plan.Total, hex.EncodeToString(digest[:])); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if invalidator, ok := h.admin.(interface{ InvalidateBalanceAfterCredit(context.Context, int64) }); ok {
		for userID := range credited {
			invalidator.InvalidateBalanceAfterCredit(cacheCtx, userID)
		}
	}
	return true, nil
}

func (h *Handler) startBlindboxWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(h.background, 2*time.Minute)
			rows, err := h.db.QueryContext(ctx, `SELECT id FROM workspace_blindbox_rounds WHERE status='open' AND draw_at<=clock_timestamp() ORDER BY id LIMIT 10`)
			ids := []int64{}
			if err == nil {
				for rows.Next() {
					var id int64
					if err = rows.Scan(&id); err != nil {
						break
					}
					ids = append(ids, id)
				}
				if err == nil {
					err = rows.Err()
				}
				rows.Close()
			}
			if err == nil {
				for _, id := range ids {
					if _, err = h.settleBlindbox(ctx, id); err != nil && ctx.Err() == nil {
						slog.Error("每日盲盒结算未完成", "round_id", id, "error_type", fmt.Sprintf("%T", err))
					}
				}
			}
			cancel()
			select {
			case <-h.background.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
