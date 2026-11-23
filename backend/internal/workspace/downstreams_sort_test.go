package workspace

import (
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestDownstreamSortUsesNumericFieldsAndStableGlobalPagination(t *testing.T) {
	for _, key := range []string{"user", "balance", "outstanding", "requests", "tokens", "cost", "last_used_at"} {
		clause, early, err := downstreamOrderClause(key, "asc")
		require.NoError(t, err)
		require.NotContains(t, clause, "::text")
		require.Contains(t, clause, "ASC NULLS LAST")
		require.Contains(t, clause, "s.id DESC")
		require.Equal(t, key == "user" || key == "balance", early)
	}
	_, _, err := downstreamOrderClause("cost;DROP TABLE users", "desc")
	require.Error(t, err)
	_, _, err = downstreamOrderClause("cost", "desc;SELECT 1")
	require.Error(t, err)
	for _, tc := range []struct{ key, expression string }{{"cost", "s.cost"}, {"outstanding", "s.outstanding"}} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery("SELECT COUNT").WithArgs("").WillReturnRows(sqlmock.NewRows([]string{"count", "balance"}).AddRow(30, "100"))
		mock.ExpectQuery(`(?s)WITH selected AS \(SELECT.*?\),.*?measured AS.*?FROM measured s ORDER BY `+tc.expression+` DESC NULLS LAST, s.id DESC LIMIT \$2 OFFSET \$3`).WithArgs("", 20, 20, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id", "email", "username", "status", "balance", "outstanding", "requests", "tokens", "cost", "last_used", "rates"}))
		router := gin.New()
		router.GET("/downstreams", (&Handler{db: db}).listDownstreams)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/downstreams?page=2&sort_by="+tc.key+"&sort_order=desc", nil))
		require.Equal(t, 200, response.Code, response.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
		db.Close()
	}
}
