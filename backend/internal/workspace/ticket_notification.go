package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

var ticketNotificationKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type ticketNotificationPayload struct {
	UserID     int64  `json:"user_id"`
	RequestKey string `json:"request_key"`
	ticketPayload
}

func normalizeNewTicket(p *ticketPayload) ([]ticketAttachment, error) {
	var err error
	p.Subject, err = trimText(p.Subject, 300)
	if err != nil {
		return nil, err
	}
	if p.Subject == "" {
		return nil, badRequest("请填写工单标题。")
	}
	if p.Category == "" {
		p.Category = "general"
	}
	if p.Priority == "" {
		p.Priority = "normal"
	}
	if !ticketCategories[p.Category] || !ticketPriorities[p.Priority] {
		return nil, badRequest("请检查问题类型和优先级。")
	}
	return prepareTicketPayload(p)
}

func decodeTicketNotification(c *gin.Context) (ticketNotificationPayload, []ticketAttachment, error) {
	var p ticketNotificationPayload
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, nil, badRequest("通知内容无法读取，请检查后重试。")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return p, nil, badRequest("请只提交一条通知。")
	}
	if p.UserID <= 0 {
		return p, nil, badRequest("请先选择收件用户。")
	}
	if !ticketNotificationKey.MatchString(p.RequestKey) {
		return p, nil, badRequest("发送编号无法读取，请重新打开通知窗口。")
	}
	images, err := normalizeNewTicket(&p.ticketPayload)
	return p, images, err
}

func (h *Handler) createTicketNotification(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	p, images, err := decodeTicketNotification(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, email, duplicate, err := h.storeTicketNotification(c.Request.Context(), a, p, images)
	if err != nil {
		fail(c, err)
		return
	}
	if !duplicate {
		h.notifyTicket(a, id, "管理员通知："+p.Subject, p.Content, email)
	}
	response.Success(c, gin.H{"id": id, "already_sent": duplicate})
}

func (h *Handler) storeTicketNotification(ctx context.Context, a actor, p ticketNotificationPayload, images []ticketAttachment) (int64, string, bool, error) {
	if !a.Admin {
		return 0, "", false, notFound()
	}
	raw, err := json.Marshal(struct {
		UserID  int64
		Payload ticketPayload
	}{p.UserID, p.ticketPayload})
	if err != nil {
		return 0, "", false, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", false, err
	}
	defer tx.Rollback()
	var email, name string
	// 重新确认当前用户存在且可登录，并锁住账号，避免查询后被删除导致通知错投或丢失。
	if err = tx.QueryRowContext(ctx, `SELECT email,username FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR SHARE`, p.UserID).Scan(&email, &name); err != nil {
		if err == sql.ErrNoRows {
			err = badRequest("该用户已停用或不存在，请重新选择。")
		}
		return 0, "", false, err
	}
	// 同一管理员的同一请求只创建一张工单；并发和断网重试均由数据库唯一键串行处理。
	result, err := tx.ExecContext(ctx, `INSERT INTO workspace_ticket_notifications(admin_id,request_key,user_id,payload_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.ID, p.RequestKey, p.UserID, hash)
	if err != nil {
		return 0, "", false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, "", false, err
	}
	var priorUser int64
	var priorHash string
	var priorTicket sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT user_id,payload_hash,ticket_id FROM workspace_ticket_notifications WHERE admin_id=$1 AND request_key=$2 FOR UPDATE`, a.ID, p.RequestKey).Scan(&priorUser, &priorHash, &priorTicket); err != nil {
		return 0, "", false, err
	}
	if priorUser != p.UserID || priorHash != hash {
		return 0, "", false, &requestError{409, "这次发送的收件人或内容已改变，请重新发起通知。"}
	}
	if inserted == 0 {
		if !priorTicket.Valid {
			return 0, "", false, &requestError{409, "这条通知曾经发送并已被清理，请重新发起通知。"}
		}
		return priorTicket.Int64, "", true, tx.Commit()
	}
	var id, messageID int64
	// 通知由管理员发起，用户保持未读；不写每日提单名额，用户仍可正常提交自己的问题。
	err = tx.QueryRowContext(ctx, `INSERT INTO workspace_tickets(user_id,user_email,user_name,subject,category,priority,status,origin,admin_last_read_at)
		VALUES($1,$2,$3,$4,$5,$6,'waiting_user','admin',clock_timestamp()) RETURNING id`, p.UserID, email, name, p.Subject, p.Category, p.Priority).Scan(&id)
	if err == nil {
		err = tx.QueryRowContext(ctx, `INSERT INTO workspace_ticket_messages(ticket_id,author_id,author_role,author_name,content) VALUES($1,$2,'admin',$3,$4) RETURNING id`, id, a.ID, a.Name, p.Content).Scan(&messageID)
	}
	if err == nil {
		err = storeTicketImages(ctx, tx, id, messageID, images)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE workspace_ticket_notifications SET ticket_id=$3 WHERE admin_id=$1 AND request_key=$2`, a.ID, p.RequestKey, id)
	}
	if err != nil {
		return 0, "", false, err
	}
	return id, email, false, tx.Commit()
}
