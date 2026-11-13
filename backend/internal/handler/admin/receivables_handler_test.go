//go:build unit

package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/pkg/adminaccess"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type receivableHandlerService struct {
	service.AdminService
	calls   int
	actorID int64
	key     string
	userID  int64
	amount  float64
	notes   string
}

func (s *receivableHandlerService) CreditUserOnAccount(_ context.Context, id int64, amount float64, notes string, actor int64, key string) (*service.User, error) {
	s.calls++
	s.actorID = actor
	s.key = key
	s.userID = id
	s.amount = amount
	s.notes = notes
	return &service.User{ID: id, Balance: amount}, nil
}
func TestOnAccountHandlerUsesAuthenticatedActorAndRejectsOtherOperations(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body string
		restricted bool
		status     int
	}{
		{"credit", `{"balance":12.12345678,"operation":"add","on_account":true,"notes":"monthly credit","operation_key":"11111111-1111-4111-8111-111111111111","actor_id":999}`, false, 200},
		{"gift", `{"balance":1,"operation":"add","on_account":true,"gift":true}`, false, 400},
		{"withdrawal", `{"balance":1,"operation":"subtract","on_account":true}`, false, 400},
		{"set", `{"balance":1,"operation":"set","on_account":true}`, false, 400},
		{"scoped credential", `{"balance":1,"operation":"add","on_account":true}`, true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &receivableHandlerService{}
			h := NewUserHandler(svc, nil, nil, nil, nil, nil, nil)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 9})
				if tc.restricted {
					c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
				}
			})
			r.POST("/users/:id/balance", h.UpdateBalance)
			request := httptest.NewRequest("POST", "/users/8/balance", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				require.Equal(t, 1, svc.calls)
				require.Equal(t, int64(9), svc.actorID)
				require.Equal(t, int64(8), svc.userID)
				require.Equal(t, 12.12345678, svc.amount)
				require.Equal(t, "monthly credit", svc.notes)
				require.Equal(t, "11111111-1111-4111-8111-111111111111", svc.key)
			} else {
				require.Zero(t, svc.calls)
			}
		})
	}
}
