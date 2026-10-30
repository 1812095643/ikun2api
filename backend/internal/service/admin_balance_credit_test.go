package service

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCommittedBalanceCreditPreservesNormalUserGuard(t *testing.T) {
	for _, committed := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBegin()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		query := `UPDATE users SET balance=balance+$1::numeric,updated_at=NOW() WHERE id=$2`
		if !committed {
			query += ` AND deleted_at IS NULL`
		}
		query += ` RETURNING balance`
		mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("2.5", int64(8)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10))
		mock.ExpectQuery("INSERT INTO redeem_codes").WithArgs("BWIN-1", "2.5", int64(8), "test", "lottery").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
		if committed {
			_, _, err = CreditCommittedBalanceTx(context.Background(), tx, 8, "2.5", "BWIN-1", "test")
		} else {
			_, _, err = CreditAdminBalanceTx(context.Background(), tx, 8, "2.5", "BWIN-1", "test", "lottery")
		}
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectRollback()
		_ = tx.Rollback()
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
