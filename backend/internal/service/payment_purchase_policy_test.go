//go:build unit

package service

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionPurchasePolicyOnlyRestrictsEnabledDownstreams(t *testing.T) {
	for _, tc := range []struct {
		downstream, restricted bool
		order                  string
		denied                 bool
	}{
		{true, false, "subscription", false}, {true, true, "subscription", true},
		{true, true, "balance", false}, {false, true, "subscription", false},
	} {
		err := validateUserSubscriptionPurchase(&User{IsDownstream: tc.downstream, DownstreamRechargeOnly: tc.restricted}, tc.order)
		require.Equal(t, tc.denied, err != nil)
	}
}

func TestSubscriptionPurchasePolicyRechecksLockedUserInsideOrderTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB("postgres", db)))
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT .*FROM "users".*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "is_downstream", "downstream_recharge_only"}).AddRow(8, true, true))
	mock.ExpectRollback()
	service := &PaymentService{entClient: client}
	_, err = service.createOrderInTx(context.Background(), CreateOrderRequest{UserID: 8, OrderType: "subscription"}, &User{ID: 8}, nil, &PaymentConfig{}, 1, 1, 0, 1, 0, nil)
	require.ErrorContains(t, err, "当前账号仅支持余额充值")
	require.NoError(t, mock.ExpectationsWereMet())
}
