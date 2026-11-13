package workspace

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/adminaccess"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func requireFinanceAdmin(c *gin.Context) {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.Forbidden(c, "仅站点管理员可访问")
		c.Abort()
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Next()
}
func (h *Handler) registerBusinessAdmin(r *gin.RouterGroup) {
	ledger := r.Group("/receivables", requireFinanceAdmin)
	ledger.GET("", h.listReceivables)
	ledger.GET("/:id", h.receivableHistory)
	ledger.POST("/:id/payments", h.recordReceivablePayment)
	downstream := r.Group("/downstreams", requireFinanceAdmin)
	downstream.GET("", h.listDownstreams)
	downstream.POST("/apply", h.applyDownstreamChanges)
	downstream.GET("/:id/usage", h.downstreamUsage)
}

type receivableAccount struct {
	UserID       int64      `json:"user_id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	Deleted      bool       `json:"deleted"`
	IsDownstream bool       `json:"is_downstream"`
	Balance      string     `json:"balance"`
	Credited     string     `json:"credited"`
	Received     string     `json:"received"`
	Outstanding  string     `json:"outstanding"`
	LastCredit   *time.Time `json:"last_credit_at"`
	LastPayment  *time.Time `json:"last_payment_at"`
}

const receivableAccountsSQL = `WITH debt AS (
 SELECT user_id,SUM(CASE WHEN kind='credit' THEN amount ELSE 0 END) credited,
 SUM(CASE WHEN kind='payment' THEN amount ELSE 0 END) received,
 SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END) outstanding,
 MAX(created_at) FILTER (WHERE kind='credit') last_credit,MAX(occurred_at) FILTER(WHERE kind='payment') last_payment
 FROM admin_receivable_entries GROUP BY user_id
), filtered AS (SELECT u.id user_id,u.email,u.username,u.deleted_at IS NOT NULL deleted,u.is_downstream,u.balance,d.credited,d.received,d.outstanding,d.last_credit,d.last_payment
 FROM debt d JOIN users u ON u.id=d.user_id
 WHERE ($1='' OR u.email ILIKE '%'||$1||'%' OR u.username ILIKE '%'||$1||'%')
 AND ($2='all' OR ($2='open' AND d.outstanding>0) OR ($2='settled' AND d.outstanding=0))) `

func (h *Handler) listReceivables(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 300 {
		fail(c, badRequest("搜索内容过长"))
		return
	}
	state := c.DefaultQuery("state", "open")
	if state != "all" && state != "open" && state != "settled" {
		fail(c, badRequest("状态无效"))
		return
	}
	var total, debtors int
	var outstanding, credited, received string
	if err := h.db.QueryRowContext(ctx, receivableAccountsSQL+`SELECT COUNT(*),COUNT(*) FILTER(WHERE outstanding>0),COALESCE(SUM(outstanding),0)::text,COALESCE(SUM(credited),0)::text,COALESCE(SUM(received),0)::text FROM filtered`, search, state).Scan(&total, &debtors, &outstanding, &credited, &received); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, receivableAccountsSQL+`SELECT user_id,email,username,deleted,is_downstream,balance::text,credited::text,received::text,outstanding::text,last_credit,last_payment FROM filtered ORDER BY outstanding DESC,user_id DESC LIMIT $3 OFFSET $4`, search, state, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []receivableAccount{}
	for rows.Next() {
		var a receivableAccount
		if err = rows.Scan(&a.UserID, &a.Email, &a.Username, &a.Deleted, &a.IsDownstream, &a.Balance, &a.Credited, &a.Received, &a.Outstanding, &a.LastCredit, &a.LastPayment); err != nil {
			fail(c, err)
			return
		}
		items = append(items, a)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "summary": gin.H{"outstanding": outstanding, "credited": credited, "received": received, "debtors": debtors}})
}
func (h *Handler) receivableHistory(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 30, 100)
	var account receivableAccount
	err = h.db.QueryRowContext(ctx, `SELECT id,email,username,deleted_at IS NOT NULL,is_downstream,balance::text FROM users WHERE id=$1`, id).Scan(&account.UserID, &account.Email, &account.Username, &account.Deleted, &account.IsDownstream, &account.Balance)
	if err != nil {
		fail(c, err)
		return
	}
	var total int
	err = h.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount) FILTER(WHERE kind='credit'),0)::text,COALESCE(SUM(amount) FILTER(WHERE kind='payment'),0)::text,COALESCE(SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END),0)::text FROM admin_receivable_entries WHERE user_id=$1`, id).Scan(&total, &account.Credited, &account.Received, &account.Outstanding)
	if err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT e.id,e.user_id,e.kind,e.amount::text,e.outstanding_after::text,e.actor_id,COALESCE(NULLIF(a.username,''),a.email),e.notes,e.payment_method,e.occurred_at,e.created_at,e.operation_key::text FROM admin_receivable_entries e JOIN users a ON a.id=e.actor_id WHERE e.user_id=$1 ORDER BY e.id DESC LIMIT $2 OFFSET $3`, id, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []service.ReceivableEntry{}
	for rows.Next() {
		var e service.ReceivableEntry
		if err = rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.Amount, &e.OutstandingAfter, &e.ActorID, &e.ActorName, &e.Notes, &e.PaymentMethod, &e.OccurredAt, &e.CreatedAt, &e.OperationKey); err != nil {
			fail(c, err)
			return
		}
		items = append(items, e)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"account": account, "items": items, "total": total, "page": page, "page_size": size})
}
func (h *Handler) recordReceivablePayment(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	actor, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var req struct {
		Amount     string     `json:"amount"`
		Notes      string     `json:"notes"`
		Method     string     `json:"payment_method"`
		Key        string     `json:"operation_key"`
		OccurredAt *time.Time `json:"occurred_at"`
	}
	if err = body(c, &req, 16<<10); err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	entry, err := service.RecordReceivablePaymentTx(c.Request.Context(), tx, id, req.Amount, req.Notes, req.Method, actor.ID, req.Key, req.OccurredAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(c, err)
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, entry)
}
