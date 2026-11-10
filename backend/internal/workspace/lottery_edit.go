package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// A revision identifies the last signed rule set without exposing its signature.
func lotteryRevision(item lotteryActivity) string {
	sum := sha256.Sum256([]byte("lottery-edit-v1\x00" + item.Status + "\x00" + item.Signature))
	return hex.EncodeToString(sum[:])
}

func validateLotteryAmendment(previous lotteryActivity, next lotterySpec, entries int, now time.Time) error {
	if previous.Status != "open" {
		return &requestError{409, "仅草稿或报名中的活动可以修改。"}
	}
	if lotteryDue(previous, entries, now) {
		return &requestError{409, "活动已达到开奖条件，请刷新查看结果。"}
	}
	if next.DrawAt != nil && !next.DrawAt.After(now) {
		return badRequest("开奖时间必须晚于当前时间。")
	}
	if next.Capacity > 0 && next.Capacity < entries {
		return badRequest("报名上限不能少于已报名人数。")
	}
	if entries == 0 {
		return nil
	}
	if next.BusinessDate != previous.Spec.BusinessDate || !next.StartAt.Equal(previous.Spec.StartAt) {
		return badRequest("已有用户报名，不能修改统计日期和报名开始时间。")
	}
	prizes := make(map[string]lotteryPrize, len(next.Prizes))
	for _, p := range next.Prizes {
		prizes[p.Key] = p
	}
	for _, before := range previous.Spec.Prizes {
		after, ok := prizes[before.Key]
		if !ok || after.Count < before.Count || decimal.RequireFromString(after.Amount).LessThan(decimal.RequireFromString(before.Amount)) {
			return badRequest("已有用户报名，奖项只能增加名额或金额，不能减少或移除。")
		}
	}
	return nil
}

func verifyLotteryAmendmentEntries(ctx context.Context, tx *sql.Tx, previous lotteryActivity, next lotterySpec, key []byte) error {
	records, err := tx.QueryContext(ctx, `SELECT user_id,evidence,signature FROM workspace_lottery_entries WHERE lottery_id=$1 ORDER BY id`, previous.ID)
	if err != nil {
		return err
	}
	defer records.Close()
	for records.Next() {
		var userID int64
		var raw []byte
		var signature string
		var evidence lotteryEvidence
		if err = records.Scan(&userID, &raw, &signature); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &evidence); err != nil {
			return err
		}
		if evidence.UserID != userID || evidence.BusinessDate != previous.Spec.BusinessDate || !lotterySignatureOK(signature, lotterySign(key, "entry", []any{previous.ID, userID, evidence})) {
			return &requestError{409, "报名记录校验未通过，暂时无法修改活动。"}
		}
		if !lotteryMatches(next, evidence) {
			return badRequest("新的参与条件会排除已报名用户，请保留其参与资格。")
		}
	}
	return records.Err()
}

func (h *Handler) saveLotteryActivity(ctx context.Context, a actor, id int64, spec lotterySpec, revision string) (lotteryActivity, error) {
	item := lotteryActivity{}
	if !a.Admin || a.ID <= 0 {
		return item, &requestError{403, "仅管理员可以修改活动。"}
	}
	// Validate here as well so internal callers cannot bypass monetary checks.
	if err := normalizeLotterySpec(&spec); err != nil {
		return item, err
	}
	key, err := h.lotteryKey(ctx)
	if err != nil {
		return item, err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	action := "save"
	var audit any
	if id > 0 {
		item, err = h.loadLottery(ctx, tx, id, true, key)
		if err != nil {
			return item, err
		}
		if item.Status != "draft" && item.Status != "open" {
			return item, &requestError{409, "活动已开奖或取消，不能修改规则。"}
		}
		revision = strings.Trim(strings.TrimSpace(revision), `"`)
		if (item.Status == "open" || revision != "") && !lotterySignatureOK(revision, lotteryRevision(item)) {
			return item, &requestError{409, "活动已更新，请重新打开编辑后保存。"}
		}
		if item.Status == "open" {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_lottery_entries WHERE lottery_id=$1`, id).Scan(&count); err != nil {
				return item, err
			}
			if err = validateLotteryAmendment(item, spec, count, time.Now()); err != nil {
				return item, err
			}
			if count > 0 && (item.Spec.Match != spec.Match || !reflect.DeepEqual(item.Spec.Conditions, spec.Conditions)) {
				if err = verifyLotteryAmendmentEntries(ctx, tx, item, spec, key); err != nil {
					return item, err
				}
			}
			action = "update"
			audit = map[string]any{"before": item.Spec, "after": spec, "entries": count}
		}
		item.Spec = spec
	} else {
		item = lotteryActivity{Spec: spec, CreatedBy: a.ID, Status: "draft"}
		raw, _ := json.Marshal(spec)
		if err = tx.QueryRowContext(ctx, `INSERT INTO workspace_lotteries(spec,signature,created_by) VALUES($1,'',$2) RETURNING id`, string(raw), a.ID).Scan(&item.ID); err != nil {
			return item, err
		}
	}
	raw, _ := json.Marshal(spec)
	item.Signature = lotterySpecSignature(key, item)
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_lotteries SET spec=$1,signature=$2 WHERE id=$3`, string(raw), item.Signature, item.ID); err != nil {
		return item, err
	}
	if audit == nil {
		audit = map[string]any{"spec": spec}
	}
	if err = lotteryAudit(ctx, tx, item.ID, &a.ID, action, audit); err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}
