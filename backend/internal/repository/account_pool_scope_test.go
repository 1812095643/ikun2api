package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPoolUserQueriesEnforceStoredGroup(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	ctx := context.Background()
	now := time.Now()
	mock.ExpectQuery(`SELECT DISTINCT account_id.*user_id = \$1.*group_id = ANY`).WithArgs(int64(42), now, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(9))
	ids, err := repo.ListAccountIDsByUser(ctx, 42, now, []int64{1})
	require.NoError(t, err)
	require.Equal(t, []int64{9}, ids)
	mock.ExpectQuery(`SELECT.*account_id.*user_id = \$1.*group_id = ANY`).WithArgs(int64(42), sqlmock.AnyArg(), now, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"account_id", "requests", "tokens", "cost", "standard_cost", "user_cost"}).AddRow(9, 1, 3, 4, 5, 6))
	stats, err := repo.GetUserAccountWindowStatsBatch(ctx, 42, ids, now, []int64{1})
	require.NoError(t, err)
	require.Equal(t, int64(1), stats[9].Requests)
	require.NoError(t, mock.ExpectationsWereMet())
}
