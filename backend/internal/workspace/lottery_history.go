package workspace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type lotteryParticipation struct {
	ID             int64      `json:"id"`
	LotteryID      int64      `json:"lottery_id"`
	Title          string     `json:"title"`
	ActivityStatus string     `json:"activity_status"`
	UserID         int64      `json:"user_id"`
	Email          string     `json:"email"`
	Username       string     `json:"username"`
	JoinedAt       time.Time  `json:"joined_at"`
	DrawnAt        *time.Time `json:"drawn_at"`
	Result         string     `json:"result"`
	WinID          *int64     `json:"win_id"`
	PrizeKey       string     `json:"prize_key"`
	Amount         string     `json:"amount"`
	ClaimedAt      *time.Time `json:"claimed_at"`
	RedeemID       *int64     `json:"redeem_id"`
	Claimed        bool       `json:"claimed"`
}
type lotteryParticipationSummary struct {
	Entries        int64  `json:"entries"`
	Pending        int64  `json:"pending"`
	Won            int64  `json:"won"`
	Lost           int64  `json:"lost"`
	Cancelled      int64  `json:"cancelled"`
	Credited       int64  `json:"credited"`
	Unclaimed      int64  `json:"unclaimed"`
	Amount         string `json:"amount"`
	CreditedAmount string `json:"credited_amount"`
}
type lotteryHistoryQuery struct {
	LotteryID int64
	Activity  string
	Search    string
	Result    string
}

const lotteryHistoryFrom = ` FROM workspace_lottery_entries e
JOIN workspace_lotteries l ON l.id=e.lottery_id
JOIN users u ON u.id=e.user_id
LEFT JOIN workspace_lottery_winners w ON w.entry_id=e.id`

// 用户范围只能来自已验证身份；查询参数不接受任意用户编号或管理员范围。
func lotteryHistoryScope(a actor, q lotteryHistoryQuery) (string, []any, error) {
	where := []string{"l.status <> 'draft'"}
	args := []any{}
	bind := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
	if !a.Admin {
		if a.ID <= 0 {
			return "", nil, &requestError{401, "请先登录。"}
		}
		where = append(where, "e.user_id="+bind(a.ID))
	}
	if q.LotteryID > 0 {
		where = append(where, "e.lottery_id="+bind(q.LotteryID))
	}
	if q.Activity != "" {
		arg := bind(q.Activity)
		where = append(where, "(l.spec->>'title' ILIKE '%'||"+arg+"||'%' OR l.id::text="+arg+")")
	}
	if q.Search != "" && a.Admin {
		arg := bind(q.Search)
		where = append(where, "(u.email ILIKE '%'||"+arg+"||'%' OR u.username ILIKE '%'||"+arg+"||'%' OR e.user_id::text="+arg+")")
	}
	return " WHERE " + strings.Join(where, " AND "), args, nil
}

func lotteryHistoryResultFilter(result string) (string, error) {
	switch result {
	case "":
		return "TRUE", nil
	case "pending":
		return "l.status='open'", nil
	case "won":
		return "w.id IS NOT NULL", nil
	case "lost":
		return "l.status='drawn' AND w.id IS NULL", nil
	case "cancelled":
		return "l.status='cancelled'", nil
	case "credited":
		return "w.claimed_at IS NOT NULL", nil
	case "unclaimed":
		return "w.id IS NOT NULL AND w.claimed_at IS NULL", nil
	default:
		return "", badRequest("请选择有效的参与结果。")
	}
}

func (h *Handler) listLotteryParticipations(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	q := lotteryHistoryQuery{Activity: strings.TrimSpace(c.Query("activity")), Search: strings.TrimSpace(c.Query("search")), Result: c.Query("result")}
	if len([]rune(q.Activity)) > 100 || len([]rune(q.Search)) > 100 {
		fail(c, badRequest("搜索内容请控制在 100 字以内。"))
		return
	}
	raw := c.Query("lottery_id")
	if c.Param("id") != "" {
		raw = c.Param("id")
	}
	if raw != "" {
		q.LotteryID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || q.LotteryID <= 0 {
			fail(c, badRequest("请检查活动编号。"))
			return
		}
	}
	where, args, err := lotteryHistoryScope(a, q)
	if err != nil {
		fail(c, err)
		return
	}
	resultFilter, err := lotteryHistoryResultFilter(q.Result)
	if err != nil {
		fail(c, err)
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 1000000), pageNumber(c.Query("page_size"), 20, 100)
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	var total int64
	summary := lotteryParticipationSummary{}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE `+resultFilter+`),count(*),
count(*) FILTER(WHERE l.status='open'),count(*) FILTER(WHERE w.id IS NOT NULL),
count(*) FILTER(WHERE l.status='drawn' AND w.id IS NULL),count(*) FILTER(WHERE l.status='cancelled'),
count(*) FILTER(WHERE w.claimed_at IS NOT NULL),count(*) FILTER(WHERE w.id IS NOT NULL AND w.claimed_at IS NULL),
COALESCE(sum(w.amount),0)::text,COALESCE(sum(w.amount) FILTER(WHERE w.claimed_at IS NOT NULL),0)::text`+lotteryHistoryFrom+where, args...).Scan(&total, &summary.Entries, &summary.Pending, &summary.Won, &summary.Lost, &summary.Cancelled, &summary.Credited, &summary.Unclaimed, &summary.Amount, &summary.CreditedAmount)
	if err != nil {
		fail(c, err)
		return
	}
	limit, offset := fmt.Sprintf("$%d", len(args)+1), fmt.Sprintf("$%d", len(args)+2)
	args = append(args, size, (page-1)*size)
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.lottery_id,l.spec->>'title',l.status,e.user_id,u.email,u.username,e.joined_at,l.drawn_at,
CASE WHEN l.status='cancelled' THEN 'cancelled' WHEN w.id IS NOT NULL THEN 'won' WHEN l.status='drawn' THEN 'lost' ELSE 'pending' END,
w.id,COALESCE(w.prize_key,''),COALESCE(w.amount,0)::text,w.claimed_at,w.redeem_id,l.created_by,l.spec,l.signature,w.signature`+lotteryHistoryFrom+where+" AND ("+resultFilter+") ORDER BY e.id DESC LIMIT "+limit+" OFFSET "+offset, args...)
	if err != nil {
		fail(c, err)
		return
	}
	items := []lotteryParticipation{}
	for rows.Next() {
		item := lotteryParticipation{}
		var createdBy int64
		var specJSON []byte
		var specSignature string
		var winSignature sql.NullString
		err = rows.Scan(&item.ID, &item.LotteryID, &item.Title, &item.ActivityStatus, &item.UserID, &item.Email, &item.Username, &item.JoinedAt, &item.DrawnAt, &item.Result, &item.WinID, &item.PrizeKey, &item.Amount, &item.ClaimedAt, &item.RedeemID, &createdBy, &specJSON, &specSignature, &winSignature)
		if err != nil {
			rows.Close()
			fail(c, err)
			return
		}
		item.Claimed = item.ClaimedAt != nil
		activity := lotteryActivity{ID: item.LotteryID, CreatedBy: createdBy}
		if err = json.Unmarshal(specJSON, &activity.Spec); err != nil {
			rows.Close()
			fail(c, err)
			return
		}
		activity.Signature = specSignature
		if !lotterySignatureOK(specSignature, lotterySpecSignature(key, activity)) {
			rows.Close()
			fail(c, &requestError{409, "参与记录对应的活动规则校验未通过。"})
			return
		}
		if item.WinID != nil {
			amount, parseErr := decimal.NewFromString(item.Amount)
			if parseErr != nil {
				rows.Close()
				fail(c, parseErr)
				return
			}
			item.Amount = amount.String()
			winner := lotteryWin{LotteryID: item.LotteryID, EntryID: item.ID, UserID: item.UserID, PrizeKey: item.PrizeKey, Amount: item.Amount}
			if !winSignature.Valid || !lotterySignatureOK(winSignature.String, lotteryWinSignature(key, winner)) {
				rows.Close()
				fail(c, &requestError{409, "中奖记录校验未通过。"})
				return
			}
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "summary": summary})
}
