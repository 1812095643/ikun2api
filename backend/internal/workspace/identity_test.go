package workspace

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workspaceIdentityAdmin struct {
	service.AdminService
	user  *service.User
	err   error
	calls int
}

func (a *workspaceIdentityAdmin) GetUserIdentity(_ context.Context, id int64) (*service.User, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	if a.user.ID != id {
		return nil, service.ErrUserNotFound
	}
	return a.user, nil
}

func (*workspaceIdentityAdmin) GetUser(context.Context, int64) (*service.User, error) {
	panic("workspace identity must not load admin usage statistics")
}

func TestWorkspaceIdentityUsesProfileWithoutUsageStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, admin := range []bool{false, true} {
		a := &workspaceIdentityAdmin{user: &service.User{ID: 8, Username: "fixture", Email: "user@example.test"}}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/workspace/checkin", nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 8})
		c.Set("workspace_admin", admin)
		got, err := (&Handler{admin: a}).identity(c)
		require.NoError(t, err)
		require.Equal(t, actor{ID: 8, Name: "fixture", Email: "user@example.test", Admin: admin}, got)
		require.Equal(t, 1, a.calls)
		require.Equal(t, "no-store", c.Writer.Header().Get("Cache-Control"))
	}
}

func TestWorkspaceIdentityRejectsMissingOrFailedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/workspace/tickets", nil)
	a := &workspaceIdentityAdmin{}
	_, err := (&Handler{admin: a}).identity(c)
	require.Error(t, err)
	require.Zero(t, a.calls)

	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 8})
	a.err = errors.New("identity unavailable")
	_, err = (&Handler{admin: a}).identity(c)
	require.ErrorIs(t, err, a.err)
}
