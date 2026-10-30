package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

// Handler 将工作台能力注册到现有应用，复用认证、数据库和邮件服务。
type Handler struct {
	db           *sql.DB
	admin        service.AdminService
	oauth        *service.OpenAIOAuthService
	encryptor    service.SecretEncryptor
	email        *service.EmailService
	settings     *service.SettingService
	quota        *service.OpenAIQuotaService
	invalidator  service.TokenCacheInvalidator
	client       *http.Client
	feedMu       sync.Mutex
	feedCache    map[string]cachedFeed
	feedRequests singleflight.Group
	runMu        sync.Mutex
	activeRuns   map[string]bool
	cfg          *config.Config
	startOnce    sync.Once
	background   context.Context
	stop         context.CancelFunc
	balanceWake  chan struct{}
}

func NewHandler(db *sql.DB, admin service.AdminService, oauth *service.OpenAIOAuthService, encryptor service.SecretEncryptor, email *service.EmailService, settings *service.SettingService, quota *service.OpenAIQuotaService, invalidator service.TokenCacheInvalidator, cfg *config.Config) *Handler {
	h := &Handler{db: db, admin: admin, oauth: oauth, encryptor: encryptor, email: email, settings: settings, quota: quota, invalidator: invalidator, cfg: cfg, feedCache: make(map[string]cachedFeed), activeRuns: make(map[string]bool)}
	h.client = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	h.background, h.stop = context.WithCancel(context.Background())
	h.balanceWake = make(chan struct{}, 1)
	return h
}

func (h *Handler) Stop() {
	if h.stop != nil {
		h.stop()
	}
}

type actor struct {
	ID    int64
	Name  string
	Email string
	Admin bool
}
type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string { return e.message }
func badRequest(message string) error { return &requestError{400, message} }
func notFound() error                 { return &requestError{404, "内容不存在或无权访问。"} }

func fail(c *gin.Context, err error) {
	var e *requestError
	if errors.As(err, &e) {
		c.JSON(e.status, gin.H{"code": e.status, "message": e.message})
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, notFound())
		return
	}
	slog.Error("工作台请求未完成", "path", c.FullPath(), "error_type", strconv.Itoa(http.StatusInternalServerError))
	response.InternalError(c, "暂时无法完成，请稍后重试。")
}

func (h *Handler) identity(c *gin.Context) (actor, error) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return actor{}, &requestError{401, "请先登录。"}
	}
	u, err := h.admin.GetUser(c.Request.Context(), subject.UserID)
	if err != nil {
		return actor{}, err
	}
	return actor{ID: u.ID, Name: u.Username, Email: u.Email, Admin: c.GetBool("workspace_admin")}, nil
}

func body(c *gin.Context, value any, limit int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	err := json.NewDecoder(c.Request.Body).Decode(value)
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		return &requestError{413, "内容太大，请分次提交。"}
	}
	if err != nil {
		return badRequest("内容格式无法读取，请检查后重试。")
	}
	return nil
}

func positiveID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("请检查记录编号。")
	}
	return id, nil
}

func pageNumber(raw string, fallback, maximum int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	if n > maximum {
		return maximum
	}
	return n
}

func (h *Handler) getRuntime(ctx context.Context, key string, target any) error {
	var raw []byte
	err := h.db.QueryRowContext(ctx, "SELECT value FROM workspace_runtime WHERE key=$1", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (h *Handler) setRuntime(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, "INSERT INTO workspace_runtime(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", key, string(raw))
	return err
}

// Register 为用户和管理员分别绑定现有鉴权，原页面不再通过 iframe 或 URL 令牌访问。
func (h *Handler) Register(v1 *gin.RouterGroup, jwt, admin, audit, backendGuard, limit, stepUp gin.HandlerFunc) {
	h.startBackground()
	user := v1.Group("/workspace", jwt, backendGuard, limit, audit)
	user.GET("/navigation", h.navigation)
	user.POST("/navigation/read", h.readNavigation)
	user.GET("/tickets", h.listTickets)
	user.POST("/tickets", h.createTicket)
	user.GET("/tickets/:id", h.getTicket)
	user.POST("/tickets/:id/messages", h.replyTicket)
	user.POST("/tickets/:id/status", h.changeTicketStatus)
	user.POST("/tickets/:id/read", h.readTicket)
	user.GET("/tickets/:id/images/:image", h.ticketImage)
	user.GET("/news/*path", h.newsFeed)
	user.GET("/reset/*path", h.resetFeed)
	user.GET("/favorites", h.listFavorites)
	user.PUT("/favorites/:id", h.saveFavorite)
	user.DELETE("/favorites/:id", h.removeFavorite)
	user.GET("/lotteries", h.listLotteries)
	user.GET("/lotteries/:id", h.getLottery)
	user.POST("/lotteries/:id/join", h.joinLottery)
	user.GET("/lottery-wins", h.listLotteryWins)
	user.GET("/lottery-entries", h.listLotteryParticipations)
	user.GET("/lottery-wins/:id", h.getLotteryWin)
	user.POST("/lottery-wins/:id/claim", h.claimLotteryWin)
	user.GET("/checkin", h.getCheckin)
	user.POST("/checkin", h.submitCheckin)
	user.GET("/blindbox", h.getBlindbox)
	user.POST("/blindbox/buy", h.buyBlindbox)
	user.GET("/blindbox/entries", h.listBlindboxEntries)
	user.POST("/blindbox/entries/:id/open", h.openBlindbox)
	manage := v1.Group("/admin/workspace", admin, limit, audit, middleware.AdminComplianceGuard(h.settings), func(c *gin.Context) { c.Set("workspace_admin", true); c.Next() })
	manage.GET("/navigation", h.navigation)
	manage.GET("/upstream-balances", h.listUpstreamBalances)
	manage.PUT("/upstream-balances/:id", h.saveUpstreamBalance)
	manage.POST("/upstream-balances/refresh", h.refreshUpstreamBalances)
	manage.POST("/upstream-balances/:id/refresh", h.refreshUpstreamBalances)
	manage.POST("/navigation/read", h.readNavigation)
	manage.GET("/checkin/settings", h.getCheckinSettings)
	manage.PUT("/checkin/settings", h.saveCheckinSettings)
	manage.GET("/blindbox/settings", h.getBlindboxSettings)
	manage.PUT("/blindbox/settings", h.saveBlindboxSettings)
	manage.GET("/blindbox/rounds", h.listBlindboxRounds)
	manage.GET("/blindbox/entries", h.listBlindboxEntries)
	manage.GET("/tickets", h.listTickets)
	manage.POST("/tickets/notify", h.createTicketNotification)
	manage.POST("/tickets/cleanup", h.cleanupTickets)
	manage.DELETE("/tickets/:id", h.deleteTicket)
	manage.GET("/lotteries", h.listLotteries)
	manage.GET("/lottery-entries", h.listLotteryParticipations)
	manage.POST("/lotteries", h.saveLottery)
	manage.POST("/lotteries/preview", h.previewLottery)
	manage.GET("/lotteries/:id", h.getLottery)
	manage.PUT("/lotteries/:id", h.saveLottery)
	manage.POST("/lotteries/:id/publish", h.publishLottery)
	manage.POST("/lotteries/:id/cancel", h.cancelLottery)
	manage.GET("/lotteries/:id/entries", h.listLotteryParticipations)
	manage.GET("/tickets/:id", h.getTicket)
	manage.POST("/tickets/:id/messages", h.replyTicket)
	manage.POST("/tickets/:id/status", h.changeTicketStatus)
	manage.POST("/tickets/:id/read", h.readTicket)
	manage.GET("/tickets/:id/images/:image", h.ticketImage)
	manage.DELETE("/tickets/:id/images/:image", h.deleteTicketImage)
	manage.GET("/relogin/accounts", h.reloginAccounts)
	manage.GET("/relogin/imported", h.listImported)
	manage.POST("/relogin/imported", h.importAccounts)
	manage.POST("/relogin/imported/export", stepUp, h.exportImported)
	manage.POST("/relogin/imported/delete", h.deleteImported)
	manage.GET("/relogin/imported/invalid", h.previewInvalidImported)
	manage.POST("/relogin/imported/invalid", h.deleteInvalidImported)
	manage.GET("/relogin/history", h.listRuns)
	manage.GET("/relogin/history/:id", h.getRun)
	manage.POST("/relogin/history/:id/export", stepUp, h.exportRun)
	manage.POST("/relogin/history/clear", h.clearRuns)
	manage.POST("/relogin/start", h.startRelogin)
	manage.POST("/relogin/resume/:id", h.resumeRelogin)
	manage.POST("/relogin/apply/:id", h.applyRelogin)
	manage.POST("/relogin/refresh-token", h.importRefreshToken)
	manage.GET("/relogin/settings", h.reloginSettings)
	manage.PUT("/relogin/settings", h.saveReloginSettings)
	manage.GET("/relogin/options", h.reloginOptions)
	manage.POST("/relogin/auto", h.runAutomaticRelogin)
}

func trimText(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > maximum {
		return "", badRequest("内容超过字数限制，请分次提交。")
	}
	return value, nil
}
