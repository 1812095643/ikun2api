package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type ticketQuota struct {
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	CanCreate bool      `json:"can_create"`
	TicketID  int64     `json:"ticket_id"`
	ResetsAt  time.Time `json:"resets_at"`
}

func (h *Handler) ticketSubmissionQuota(ctx context.Context, userID int64) (ticketQuota, error) {
	quota := ticketQuota{Limit: 1}
	err := h.db.QueryRowContext(ctx, `WITH today AS (
		SELECT (statement_timestamp() AT TIME ZONE 'Asia/Shanghai')::date AS day
	) SELECT (SELECT count(*) FROM workspace_ticket_daily_submissions d, today WHERE d.user_id=$1 AND d.submission_day=today.day),
		COALESCE((SELECT d.ticket_id FROM workspace_ticket_daily_submissions d, today WHERE d.user_id=$1 AND d.submission_day=today.day),0),
		(today.day+1)::timestamp AT TIME ZONE 'Asia/Shanghai' FROM today`, userID).Scan(&quota.Used, &quota.TicketID, &quota.ResetsAt)
	quota.CanCreate = quota.Used == 0
	return quota, err
}

func reserveTicketSubmission(ctx context.Context, tx *sql.Tx, userID int64) (time.Time, error) {
	var day time.Time
	// 唯一键在数据库内串行分配名额，重复点击、多标签页及并发请求只能成功一次。
	err := tx.QueryRowContext(ctx, `INSERT INTO workspace_ticket_daily_submissions(user_id,submission_day)
		VALUES($1,(statement_timestamp() AT TIME ZONE 'Asia/Shanghai')::date)
		ON CONFLICT DO NOTHING RETURNING submission_day`, userID).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return day, &requestError{429, "今天已提交 1 张工单，请在原工单内继续沟通。北京时间 00:00 可新建。"}
	}
	return day, err
}

func (h *Handler) deleteTicket(c *gin.Context) {
	if _, err := h.confirmTicketCleanup(c); err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	result, err := h.db.ExecContext(c.Request.Context(), "DELETE FROM workspace_tickets WHERE id=$1", id)
	if err != nil {
		fail(c, err)
		return
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		fail(c, err)
		return
	}
	if deleted == 0 {
		fail(c, notFound())
		return
	}
	response.Success(c, gin.H{"deleted": deleted})
}

func (h *Handler) cleanupTickets(c *gin.Context) {
	if _, err := h.confirmTicketCleanup(c); err != nil {
		fail(c, err)
		return
	}
	// 只清理已解决或已关闭的工单，活跃工单需要逐张明确删除。
	result, err := h.db.ExecContext(c.Request.Context(), "DELETE FROM workspace_tickets WHERE status IN ('resolved','closed')")
	if err != nil {
		fail(c, err)
		return
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": deleted})
}

func (h *Handler) confirmTicketCleanup(c *gin.Context) (actor, error) {
	if !c.GetBool("workspace_admin") {
		return actor{}, notFound()
	}
	a, err := h.identity(c)
	if err != nil {
		return actor{}, err
	}
	var payload struct {
		Confirm bool `json:"confirm"`
	}
	if err = body(c, &payload, 1024); err != nil {
		return actor{}, err
	}
	if !payload.Confirm {
		return actor{}, badRequest("请确认删除工单及全部对话、图片。")
	}
	return a, nil
}
