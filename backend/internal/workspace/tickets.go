package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"html"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var ticketStatuses = map[string]bool{"open": true, "in_progress": true, "waiting_user": true, "resolved": true, "closed": true}
var ticketImageProcessing sync.Mutex
var ticketCategories = map[string]bool{"general": true, "account": true, "billing": true, "model": true, "bug": true}
var ticketPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
var statusLabels = map[string]string{"open": "待处理", "in_progress": "处理中", "waiting_user": "待回复", "resolved": "已解决", "closed": "已关闭"}

type ticket struct {
	ID            int64           `json:"id"`
	UserID        int64           `json:"user_id"`
	UserEmail     string          `json:"user_email"`
	UserName      string          `json:"user_name"`
	Subject       string          `json:"subject"`
	Category      string          `json:"category"`
	Priority      string          `json:"priority"`
	Status        string          `json:"status"`
	Origin        string          `json:"origin"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	EndedAt       *time.Time      `json:"ended_at"`
	Unread        int             `json:"unread_count"`
	QueuePosition int             `json:"queue_position"`
	Messages      []ticketMessage `json:"messages,omitempty"`
}
type ticketMessage struct {
	ID         int64              `json:"id"`
	AuthorID   int64              `json:"author_id"`
	AuthorRole string             `json:"author_role"`
	AuthorName string             `json:"author_name"`
	Kind       string             `json:"kind"`
	Content    string             `json:"content"`
	EventType  string             `json:"event_type"`
	CreatedAt  time.Time          `json:"created_at"`
	Images     []ticketAttachment `json:"images"`
}
type ticketAttachment struct {
	ID       string `json:"id"`
	Filename string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int    `json:"size"`
	Deleted  bool   `json:"deleted"`
	Data     []byte `json:"-"`
}
type ticketPayload struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
	Priority string `json:"priority"`
	Content  string `json:"content"`
	Images   []struct {
		Name string `json:"name"`
		Data string `json:"data"`
	} `json:"images"`
}

const ticketColumns = "t.id,t.user_id,t.user_email,t.user_name,t.subject,t.category,t.priority,t.status,t.origin,t.created_at,t.updated_at,t.ended_at"

type rowScanner interface{ Scan(...any) error }

func scanTicket(s rowScanner) (ticket, error) {
	t := ticket{}
	err := s.Scan(&t.ID, &t.UserID, &t.UserEmail, &t.UserName, &t.Subject, &t.Category, &t.Priority, &t.Status, &t.Origin, &t.CreatedAt, &t.UpdatedAt, &t.EndedAt, &t.Unread, &t.QueuePosition)
	return t, err
}

func (h *Handler) loadTicket(ctx context.Context, a actor, id int64) (ticket, error) {
	return scanTicket(h.db.QueryRowContext(ctx, "SELECT "+ticketColumns+`,(SELECT count(*) FROM workspace_ticket_messages m WHERE m.ticket_id=t.id AND m.kind='message' AND m.author_role<>$3 AND m.created_at>COALESCE(CASE WHEN $2 THEN t.admin_last_read_at ELSE t.user_last_read_at END,'epoch'::timestamptz)),COALESCE((SELECT count(*) FROM workspace_tickets q WHERE q.status IN ('open','in_progress','waiting_user') AND (q.created_at,q.id)<=(t.created_at,t.id) AND t.status IN ('open','in_progress','waiting_user')),0) FROM workspace_tickets t WHERE t.id=$1 AND ($2 OR t.user_id=$4)`, id, a.Admin, actorRole(a), a.ID))
}

func actorRole(a actor) string {
	if a.Admin {
		return "admin"
	}
	return "user"
}

func (h *Handler) listTickets(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	page := pageNumber(c.Query("page"), 1, 1000000)
	size := pageNumber(c.Query("page_size"), 20, 50)
	search := strings.TrimSpace(c.Query("search"))
	if len([]rune(search)) > 300 {
		fail(c, badRequest("搜索内容最多 300 字。"))
		return
	}
	status := c.Query("status")
	if status != "" && !ticketStatuses[status] {
		fail(c, badRequest("请检查筛选状态。"))
		return
	}
	ctx := c.Request.Context()
	where := ` WHERE ($1 OR t.user_id=$2) AND ($3='' OR t.status=$3) AND ($4='' OR t.subject ILIKE '%'||$4||'%' OR ($1 AND t.user_email ILIKE '%'||$4||'%'))`
	var total, active, unread, ended int
	if err = h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_tickets t"+where, a.Admin, a.ID, status, search).Scan(&total); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, "SELECT "+ticketColumns+`,(SELECT count(*) FROM workspace_ticket_messages m WHERE m.ticket_id=t.id AND m.kind='message' AND m.author_role<>$5 AND m.created_at>COALESCE(CASE WHEN $1 THEN t.admin_last_read_at ELSE t.user_last_read_at END,'epoch'::timestamptz)),COALESCE((SELECT count(*) FROM workspace_tickets q WHERE q.status IN ('open','in_progress','waiting_user') AND (q.created_at,q.id)<=(t.created_at,t.id) AND t.status IN ('open','in_progress','waiting_user')),0)`+" FROM workspace_tickets t"+where+" ORDER BY t.updated_at DESC,t.id DESC LIMIT $6 OFFSET $7", a.Admin, a.ID, status, search, actorRole(a), size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []ticket{}
	for rows.Next() {
		t, e := scanTicket(rows)
		if e != nil {
			fail(c, e)
			return
		}
		items = append(items, t)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	err = h.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE t.status IN ('open','in_progress','waiting_user')),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM workspace_ticket_messages m WHERE m.ticket_id=t.id AND m.kind='message' AND m.author_role<>$3 AND m.created_at>COALESCE(CASE WHEN $1 THEN t.admin_last_read_at ELSE t.user_last_read_at END,'epoch'::timestamptz))),count(*) FILTER(WHERE t.status IN ('resolved','closed')) FROM workspace_tickets t WHERE $1 OR t.user_id=$2`, a.Admin, a.ID, actorRole(a)).Scan(&active, &unread, &ended)
	if err != nil {
		fail(c, err)
		return
	}
	var queueTotal int
	if err = h.db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_tickets WHERE status IN ('open','in_progress','waiting_user')`).Scan(&queueTotal); err != nil {
		fail(c, err)
		return
	}
	result := gin.H{"items": items, "total": total, "page": page, "page_size": size, "stats": gin.H{"active": active, "unread": unread, "ended": ended}, "queue": gin.H{"total": queueTotal}}
	if !a.Admin {
		quota, quotaErr := h.ticketSubmissionQuota(ctx, a.ID)
		if quotaErr != nil {
			fail(c, quotaErr)
			return
		}
		result["quota"] = quota
	}
	response.Success(c, result)
}

func (h *Handler) getTicket(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	t, err := h.loadTicket(c.Request.Context(), a, id)
	if err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), "SELECT id,author_id,author_role,author_name,kind,content,event_type,created_at FROM workspace_ticket_messages WHERE ticket_id=$1 ORDER BY id", id)
	if err != nil {
		fail(c, err)
		return
	}
	t.Messages = []ticketMessage{}
	indices := map[int64]int{}
	for rows.Next() {
		m := ticketMessage{Images: []ticketAttachment{}}
		if err = rows.Scan(&m.ID, &m.AuthorID, &m.AuthorRole, &m.AuthorName, &m.Kind, &m.Content, &m.EventType, &m.CreatedAt); err != nil {
			rows.Close()
			fail(c, err)
			return
		}
		indices[m.ID] = len(t.Messages)
		t.Messages = append(t.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	images, err := h.db.QueryContext(c.Request.Context(), "SELECT id,message_id,filename,width,height,size_bytes,deleted_at IS NOT NULL FROM workspace_ticket_images WHERE ticket_id=$1 ORDER BY created_at,id", id)
	if err != nil {
		fail(c, err)
		return
	}
	defer images.Close()
	for images.Next() {
		var img ticketAttachment
		var messageID int64
		if err = images.Scan(&img.ID, &messageID, &img.Filename, &img.Width, &img.Height, &img.Size, &img.Deleted); err != nil {
			fail(c, err)
			return
		}
		if idx, ok := indices[messageID]; ok {
			t.Messages[idx].Images = append(t.Messages[idx].Images, img)
		}
	}
	if err = images.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, t)
}

// 图片重新解码压缩并移除元数据，保留 43 现有的 4 张、1920 边长与 512 KB 限制。
func prepareTicketPayload(p *ticketPayload) ([]ticketAttachment, error) {
	if len(p.Images) > 0 {
		ticketImageProcessing.Lock()
		defer ticketImageProcessing.Unlock()
	}
	var err error
	p.Content, err = trimText(p.Content, 50000)
	if err != nil {
		return nil, err
	}
	if len(p.Images) > 4 {
		return nil, badRequest("每次最多发送 4 张图片。")
	}
	if p.Content == "" && len(p.Images) == 0 {
		return nil, badRequest("请填写内容或添加图片。")
	}
	output := []ticketAttachment{}
	for _, input := range p.Images {
		if len(input.Data) > 1400000 {
			return nil, &requestError{413, "图片太大，请先压缩后上传。"}
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(input.Data)
		if e != nil {
			return nil, badRequest("图片无法读取，请重新选择。")
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 20000000 {
			return nil, badRequest("图片尺寸不合适，请缩小后重新选择。")
		}
		src, _, e := image.Decode(bytes.NewReader(raw))
		if e != nil {
			return nil, badRequest("图片无法解码，请重新选择。")
		}
		width, height := cfg.Width, cfg.Height
		if width > 1920 || height > 1920 {
			if width >= height {
				height = height * 1920 / width
				width = 1920
			} else {
				width = width * 1920 / height
				height = 1920
			}
			if width < 1 {
				width = 1
			}
			if height < 1 {
				height = 1
			}
		}
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		var encoded bytes.Buffer
		for _, quality := range []int{82, 70, 58, 46} {
			encoded.Reset()
			if e = jpeg.Encode(&encoded, dst, &jpeg.Options{Quality: quality}); e != nil {
				return nil, badRequest("图片暂时无法处理。")
			}
			if encoded.Len() <= 512*1024 {
				break
			}
		}
		if encoded.Len() > 512*1024 {
			return nil, &requestError{413, "图片压缩后仍超过 512 KB，请缩小后重试。"}
		}
		name, e := trimText(input.Name, 160)
		if e != nil {
			return nil, e
		}
		if name == "" {
			name = "图片"
		}
		id := make([]byte, 16)
		if _, e = rand.Read(id); e != nil {
			return nil, e
		}
		output = append(output, ticketAttachment{ID: hex.EncodeToString(id), Filename: name, Width: width, Height: height, Size: encoded.Len(), Data: append([]byte(nil), encoded.Bytes()...)})
	}
	return output, nil
}

func storeTicketImages(ctx context.Context, tx *sql.Tx, ticketID, messageID int64, images []ticketAttachment) error {
	if len(images) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(748315201)"); err != nil {
		return err
	}
	var ownBytes, totalBytes, ownCount int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(sum(size_bytes),0),COALESCE(sum(size_bytes) FILTER(WHERE ticket_id=$1),0),count(*) FILTER(WHERE ticket_id=$1 AND data IS NOT NULL) FROM workspace_ticket_images", ticketID).Scan(&totalBytes, &ownBytes, &ownCount); err != nil {
		return err
	}
	added := 0
	for _, img := range images {
		added += img.Size
	}
	if ownCount+len(images) > 40 {
		return badRequest("每张工单最多保留 40 张图片，请先删除已阅图片。")
	}
	if ownBytes+added > 8*1024*1024 || totalBytes+added > 128*1024*1024 {
		return &requestError{413, "图片存储已满，请管理员删除已阅图片；文字仍可发送。"}
	}
	for _, img := range images {
		_, err := tx.ExecContext(ctx, "INSERT INTO workspace_ticket_images(id,ticket_id,message_id,filename,width,height,size_bytes,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", img.ID, ticketID, messageID, img.Filename, img.Width, img.Height, img.Size, img.Data)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) createTicket(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var p ticketPayload
	if err = body(c, &p, 4<<20); err != nil {
		fail(c, err)
		return
	}
	images, err := normalizeNewTicket(&p)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	day, err := reserveTicketSubmission(ctx, tx, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	var id, messageID int64
	err = tx.QueryRowContext(ctx, "INSERT INTO workspace_tickets(user_id,user_email,user_name,subject,category,priority,user_last_read_at) VALUES($1,$2,$3,$4,$5,$6,NOW()) RETURNING id", a.ID, a.Email, a.Name, p.Subject, p.Category, p.Priority).Scan(&id)
	if err == nil {
		err = tx.QueryRowContext(ctx, "INSERT INTO workspace_ticket_messages(ticket_id,author_id,author_role,author_name,content) VALUES($1,$2,$3,$4,$5) RETURNING id", id, a.ID, actorRole(a), a.Name, p.Content).Scan(&messageID)
	}
	if err == nil {
		err = storeTicketImages(ctx, tx, id, messageID, images)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE workspace_ticket_daily_submissions SET ticket_id=$3 WHERE user_id=$1 AND submission_day=$2", a.ID, day, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(c, err)
		return
	}
	h.notifyTicket(a, id, "新工单："+p.Subject, p.Content, "")
	response.Success(c, gin.H{"id": id})
}

func (h *Handler) replyTicket(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var p ticketPayload
	if err = body(c, &p, 4<<20); err != nil {
		fail(c, err)
		return
	}
	images, err := prepareTicketPayload(&p)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	var status, email, subject string
	err = tx.QueryRowContext(ctx, "SELECT status,user_email,subject FROM workspace_tickets WHERE id=$1 AND ($2 OR user_id=$3) FOR UPDATE", id, a.Admin, a.ID).Scan(&status, &email, &subject)
	if err != nil {
		fail(c, err)
		return
	}
	if status == "closed" && !a.Admin {
		fail(c, &requestError{409, "工单已关闭，请先重新打开。"})
		return
	}
	next := status
	if a.Admin && status != "closed" && status != "resolved" {
		next = "waiting_user"
	}
	if !a.Admin && (status == "waiting_user" || status == "resolved") {
		next = "open"
	}
	var messageID int64
	err = tx.QueryRowContext(ctx, "INSERT INTO workspace_ticket_messages(ticket_id,author_id,author_role,author_name,content) VALUES($1,$2,$3,$4,$5) RETURNING id", id, a.ID, actorRole(a), a.Name, p.Content).Scan(&messageID)
	if err == nil {
		err = storeTicketImages(ctx, tx, id, messageID, images)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE workspace_tickets SET status=$2,updated_at=NOW(),ended_at=CASE WHEN $2 IN ('open','in_progress','waiting_user') THEN NULL ELSE ended_at END WHERE id=$1", id, next)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(c, err)
		return
	}
	h.notifyTicket(a, id, "工单回复："+subject, p.Content, email)
	response.Success(c, gin.H{"id": id, "status": next})
}

func (h *Handler) changeTicketStatus(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var p struct {
		Status string `json:"status"`
	}
	if err = body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if !ticketStatuses[p.Status] || (!a.Admin && p.Status != "open" && p.Status != "closed") {
		fail(c, badRequest("请选择允许的工单状态。"))
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	var old, email, subject string
	err = tx.QueryRowContext(ctx, "SELECT status,user_email,subject FROM workspace_tickets WHERE id=$1 AND ($2 OR user_id=$3) FOR UPDATE", id, a.Admin, a.ID).Scan(&old, &email, &subject)
	if err != nil {
		fail(c, err)
		return
	}
	if old != p.Status {
		_, err = tx.ExecContext(ctx, "UPDATE workspace_tickets SET status=$2,updated_at=NOW(),ended_at=CASE WHEN $2 IN ('resolved','closed') THEN NOW() ELSE NULL END WHERE id=$1", id, p.Status)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO workspace_ticket_messages(ticket_id,author_id,author_role,author_name,kind,content,event_type) VALUES($1,$2,'system',$3,'event',$4,'status_changed')", id, a.ID, a.Name, "状态更新为："+statusLabels[p.Status])
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(c, err)
		return
	}
	if old != p.Status {
		h.notifyTicket(a, id, "状态更新："+subject, statusLabels[p.Status], email)
	}
	response.Success(c, gin.H{"id": id, "status": p.Status})
}

func (h *Handler) readTicket(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	column := "user_last_read_at"
	if a.Admin {
		column = "admin_last_read_at"
	}
	result, err := h.db.ExecContext(c.Request.Context(), "UPDATE workspace_tickets SET "+column+"=NOW() WHERE id=$1 AND ($2 OR user_id=$3)", id, a.Admin, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		fail(c, err)
		return
	}
	if n == 0 {
		fail(c, notFound())
		return
	}
	response.Success(c, gin.H{"read": true})
}

func (h *Handler) ticketImage(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var raw []byte
	var mime string
	err = h.db.QueryRowContext(c.Request.Context(), "SELECT i.data,i.mime_type FROM workspace_ticket_images i JOIN workspace_tickets t ON t.id=i.ticket_id WHERE i.ticket_id=$1 AND i.id=$2 AND i.data IS NOT NULL AND ($3 OR t.user_id=$4)", id, c.Param("image"), a.Admin, a.ID).Scan(&raw, &mime)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, mime, raw)
}

func (h *Handler) deleteTicketImage(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var p struct {
		Confirm bool `json:"confirm"`
	}
	if err = body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if !p.Confirm {
		fail(c, badRequest("请先确认图片已经阅览。"))
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(), "UPDATE workspace_ticket_images SET data=NULL,size_bytes=0,deleted_at=NOW(),deleted_by=$3 WHERE ticket_id=$1 AND id=$2", id, c.Param("image"), a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(c, notFound())
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *Handler) notifyTicket(a actor, id int64, subject, content, userEmail string) {
	if h.email == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		recipients := []string{}
		if a.Admin {
			if userEmail != "" {
				recipients = append(recipients, userEmail)
			}
		} else {
			rows, err := h.db.QueryContext(ctx, "SELECT email FROM users WHERE role='admin' AND status='active' AND deleted_at IS NULL")
			if err != nil {
				return
			}
			defer rows.Close()
			for rows.Next() {
				var email string
				if rows.Scan(&email) == nil && email != "" {
					recipients = append(recipients, email)
				}
			}
		}
		for _, to := range recipients {
			_ = h.email.SendEmail(ctx, to, "[IKUN2api 工单] "+subject, "<p>"+html.EscapeString(a.Name)+"</p><pre style=\"white-space:pre-wrap\">"+html.EscapeString(content)+"</pre>")
		}
	}()
}
