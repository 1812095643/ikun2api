//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteRewriteKeepsOuterTransactionOwnership(t *testing.T) {
	for _, contextCarriesTx := range []bool{false, true} {
		t.Run(map[bool]string{false: "bound_client", true: "context_transaction"}[contextCarriesTx], func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			client := tx.Client()
			user := mustCreateUser(t, client, &service.User{})
			first := mustCreateGroup(t, client, &service.Group{Name: "route-first-" + uuid.NewString()})
			next := mustCreateGroup(t, client, &service.Group{Name: "route-next-" + uuid.NewString()})
			key, err := client.APIKey.Create().SetUserID(user.ID).SetKey("fixture-" + uuid.NewString()).
				SetName("route fixture").SetGroupID(first.ID).SetRouteGroupIds([]int64{first.ID, next.ID}).Save(ctx)
			require.NoError(t, err)
			if contextCarriesTx {
				ctx = dbent.NewTxContext(ctx, tx)
			}
			repo := newAPIKeyRepositoryWithSQL(client, tx)
			changed, err := repo.RemoveGroupFromSmartRoutes(ctx, first.ID)
			require.NoError(t, err)
			require.EqualValues(t, 1, changed)
			got, err := repo.GetByID(ctx, key.ID)
			require.NoError(t, err)
			require.Equal(t, []int64{next.ID}, got.CandidateGroupIDs())
			var visible int
			require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM api_keys WHERE id=$1", key.ID).Scan(&visible))
			require.Zero(t, visible, "nested route update must not commit the caller's transaction")
			require.NoError(t, tx.Rollback())
			require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM api_keys WHERE id=$1", key.ID).Scan(&visible))
			require.Zero(t, visible)
		})
	}
}
