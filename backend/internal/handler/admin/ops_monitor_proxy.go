package admin

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/pkg/adminaccess"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var monitorPath = regexp.MustCompile(`^/(summary|live|config|channels|channels/test|channels/feishu/chats|incidents/[a-zA-Z0-9_-]+(/control|/preview)?)$`)

func NewMonitorProxy(socket string) gin.HandlerFunc {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second}
	// Channel test sends and credential checks wait on Feishu/SMTP; other calls are fast.
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(c *gin.Context) {
		actor, ok := middleware.GetAuthSubjectFromContext(c)
		role, roleOK := middleware.GetUserRoleFromContext(c)
		if !ok || actor.UserID <= 0 || !roleOK || role != "admin" {
			response.Error(c, 403, "仅管理员可访问异常通知")
			return
		}
		if principal := adminaccess.FromContext(c.Request.Context()); principal != nil {
			response.Error(c, 403, "异常通知配置暂不向受限管理密钥开放")
			return
		}
		if socket == "" || !filepath.IsAbs(socket) {
			response.Error(c, 503, "独立监控尚未部署或未配置连接")
			return
		}
		path := c.Param("path")
		if !monitorPath.MatchString(path) || strings.Contains(c.Request.URL.RawPath, "%") {
			response.Error(c, 404, "接口不存在")
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "PUT" && c.Request.Method != "POST" {
			response.Error(c, 405, "不支持此方法")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
		req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, "http://monitor/v1"+path, c.Request.Body)
		if err != nil {
			response.Error(c, 400, "请求不正确")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Ops-Actor", fmt.Sprintf("admin:%d", actor.UserID))
		res, err := client.Do(req)
		if err != nil {
			response.Error(c, 503, "独立监控暂不可用；网关转发不受此接口影响")
			return
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
		if err != nil || len(raw) > 8<<20 {
			response.Error(c, 502, "监控返回超出接口限制")
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(res.StatusCode, "application/json", raw)
	}
}
func (h *OpsHandler) Monitor(c *gin.Context) {
	if h.monitorProxy == nil {
		response.Error(c, 503, "独立监控尚未配置")
		return
	}
	h.monitorProxy(c)
}
