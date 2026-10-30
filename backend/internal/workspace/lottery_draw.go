package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"time"

	"github.com/lib/pq"
)

type lotteryDrawEntry struct {
	ID     int64
	UserID int64
}

// 使用密码学随机源洗牌；读取随机源出现问题时回滚整场，不生成降级的可预测结果。
func shuffleLottery(entries []lotteryDrawEntry, source io.Reader) error {
	for i := len(entries) - 1; i > 0; i-- {
		j, err := rand.Int(source, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		entries[i], entries[j.Int64()] = entries[j.Int64()], entries[i]
	}
	return nil
}

func (h *Handler) drawLottery(ctx context.Context, id int64) (bool, error) {
	key, err := h.lotteryKey(ctx)
	if err != nil {
		return false, err
	}
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	a, err := h.loadLottery(ctx, tx, id, true, key)
	if err != nil {
		return false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_lottery_entries WHERE lottery_id=$1`, id).Scan(&count); err != nil {
		return false, err
	}
	if !lotteryDue(a, count, time.Now()) {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,evidence,signature FROM workspace_lottery_entries WHERE lottery_id=$1 ORDER BY id`, id)
	if err != nil {
		return false, err
	}
	entries := []lotteryDrawEntry{}
	for rows.Next() {
		var e lotteryDrawEntry
		var raw []byte
		var signature string
		var evidence lotteryEvidence
		if err = rows.Scan(&e.ID, &e.UserID, &raw, &signature); err != nil {
			rows.Close()
			return false, err
		}
		if err = json.Unmarshal(raw, &evidence); err != nil {
			rows.Close()
			return false, err
		}
		if evidence.UserID != e.UserID || evidence.BusinessDate != a.Spec.BusinessDate || !lotteryMatches(a.Spec, evidence) || !lotterySignatureOK(signature, lotterySign(key, "entry", []any{id, e.UserID, evidence})) {
			rows.Close()
			return false, fmt.Errorf("报名记录完整性校验未通过，活动 %d", id)
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	start, end := lotteryDateBounds(a.Spec)
	rows, err = tx.QueryContext(ctx, lotteryEvidenceSQL+` AND u.id >= $1 AND EXISTS(SELECT 1 FROM workspace_lottery_entries e WHERE e.lottery_id=$4 AND e.user_id=u.id)`, int64(1), start, end, id)
	if err != nil {
		return false, err
	}
	qualified := map[int64]bool{}
	for rows.Next() {
		e, eErr := scanLotteryEvidence(rows, a.Spec)
		if eErr != nil {
			rows.Close()
			return false, eErr
		}
		qualified[e.UserID] = lotteryMatches(a.Spec, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	eligible := make([]lotteryDrawEntry, 0, len(entries))
	for _, e := range entries {
		if qualified[e.UserID] {
			eligible = append(eligible, e)
		}
	}
	if err = shuffleLottery(eligible, rand.Reader); err != nil {
		return false, err
	}
	users, entryIDs := []int64{}, []int64{}
	prizes, amounts, signatures := []string{}, []string{}, []string{}
	wins := []lotteryWin{}
	index := 0
	for _, p := range a.Spec.Prizes {
		for i := 0; i < p.Count && index < len(eligible); i++ {
			e := eligible[index]
			index++
			w := lotteryWin{LotteryID: id, EntryID: e.ID, UserID: e.UserID, PrizeKey: p.Key, Amount: p.Amount}
			w.Signature = lotteryWinSignature(key, w)
			wins = append(wins, w)
			users = append(users, e.UserID)
			entryIDs = append(entryIDs, e.ID)
			prizes = append(prizes, p.Key)
			amounts = append(amounts, p.Amount)
			signatures = append(signatures, w.Signature)
		}
	}
	if len(wins) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_lottery_winners(lottery_id,entry_id,user_id,prize_key,amount,signature)
		SELECT $1,entry_id,user_id,prize_key,amount::numeric,signature FROM unnest($2::bigint[],$3::bigint[],$4::text[],$5::text[],$6::text[]) AS t(entry_id,user_id,prize_key,amount,signature)`, id, pq.Array(entryIDs), pq.Array(users), pq.Array(prizes), pq.Array(amounts), pq.Array(signatures))
		if err != nil {
			return false, err
		}
	}
	raw, _ := json.Marshal(wins)
	digest := sha256.Sum256(raw)
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_lotteries SET status='drawn',drawn_at=NOW(),eligible_count=$2,result_digest=$3 WHERE id=$1`, id, len(eligible), hex.EncodeToString(digest[:])); err != nil {
		return false, err
	}
	if err = lotteryAudit(ctx, tx, id, nil, "draw", map[string]any{"entries": count, "eligible": len(eligible), "winners": len(wins), "result_digest": hex.EncodeToString(digest[:])}); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) startLotteryWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(h.background, 2*time.Minute)
			rows, err := h.db.QueryContext(ctx, `SELECT id FROM workspace_lotteries WHERE status='open' ORDER BY id`)
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
					if _, drawErr := h.drawLottery(ctx, id); drawErr != nil && !errors.Is(drawErr, context.Canceled) {
						slog.Error("抽奖自动开奖未完成", "lottery_id", id, "error", drawErr)
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
