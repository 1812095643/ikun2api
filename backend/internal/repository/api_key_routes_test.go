package repository

import (
	"context"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/internal/pkg/pagination"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSmartRoutingGroupLifecyclePreservesOrder(t *testing.T) {
	require.Equal(t, []int64{2, 3}, rewriteAPIKeyRouteIDs([]int64{1, 2, 3}, 1, 0))
	require.Equal(t, []int64{3, 2}, rewriteAPIKeyRouteIDs([]int64{1, 2, 3}, 1, 3))
	require.Equal(t, []int64{1, 4, 3}, rewriteAPIKeyRouteIDs([]int64{1, 2, 3}, 2, 4))
	require.Empty(t, rewriteAPIKeyRouteIDs([]int64{1}, 1, 0))
}

func TestSmartRoutingRepositoryPersistsAndFiltersSecondaryGroupsSQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "smart-routes-local@test.invalid")
	first, err := client.Group.Create().SetName("smart-first").Save(ctx)
	require.NoError(t, err)
	second, err := client.Group.Create().SetName("smart-second").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Name: "routing", Key: "local-route-key", GroupID: &first.ID, RouteGroupIDs: []int64{first.ID, second.ID}, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	read, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.RouteGroupIDs, read.RouteGroupIDs)
	auth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.RouteGroupIDs, auth.RouteGroupIDs)
	rows, page, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 20}, service.APIKeyListFilters{GroupID: &second.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, rows, 1)
	require.Equal(t, key.RouteGroupIDs, rows[0].RouteGroupIDs)
	keys, err := repo.ListKeysByGroupID(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, []string{key.Key}, keys)
	// A route edit writes the two routing fields atomically and leaves live usage intact.
	key.GroupID = &second.ID
	key.RouteGroupIDs = []int64{second.ID, first.ID}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{GroupID: true}))
	current, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, *current.GroupID)
	require.Equal(t, key.RouteGroupIDs, current.RouteGroupIDs)
}

func TestSmartRoutingGroupRewriteRollsBackAsOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAPIKeyRepositoryWithSQL(client, db)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "api_keys".*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "route_group_ids"}).AddRow(int64(10), int64(1), []byte(`[1,2,3]`)))
	failure := errors.New("simulated write failure")
	mock.ExpectExec(`UPDATE "api_keys" SET`).WillReturnError(failure)
	mock.ExpectRollback()
	_, err = repo.ClearGroupIDByGroupID(context.Background(), 1)
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}
