//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	servermiddleware "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestUpdateMaintenanceDrainsQPSWebSocket(t *testing.T) {
	service.EndUpgradeMaintenance()
	t.Cleanup(service.EndUpgradeMaintenance)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(servermiddleware.UpgradeMaintenance())
	finished := make(chan struct{})
	router.GET("/api/v1/admin/ops/ws/qps", func(c *gin.Context) {
		defer close(finished)
		conn, err := (&websocket.Upgrader{}).Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handleQPSWebSocket(c.Request.Context(), conn)
	})
	router.GET("/api/v1/admin/system/update-status", func(c *gin.Context) { c.Status(http.StatusOK) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/admin/ops/ws/qps"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = service.BeginUpgradeMaintenance(ctx)
	require.NoError(t, err, "an open monitoring socket must drain before the update timeout")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("monitoring handler did not exit for maintenance")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseServiceRestart), "got %v", err)

	_, response, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "10", response.Header.Get("Retry-After"))
	status, err := http.Get(server.URL + "/api/v1/admin/system/update-status")
	require.NoError(t, err)
	defer status.Body.Close()
	require.Equal(t, http.StatusOK, status.StatusCode)
}
