package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestTicketQuotaReservationConflictAndRollback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("INSERT INTO workspace_ticket_daily_submissions").WithArgs(int64(8)).WillReturnError(sql.ErrNoRows)
	_, err = reserveTicketSubmission(context.Background(), tx, 8)
	var limited *requestError
	if !errors.As(err, &limited) || limited.status != 429 {
		t.Fatalf("duplicate submission was accepted: %v", err)
	}
	mock.ExpectRollback()
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	tx, err = db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("INSERT INTO workspace_ticket_daily_submissions").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"submission_day"}).AddRow(day))
	if _, err = reserveTicketSubmission(context.Background(), tx, 9); err != nil {
		t.Fatal(err)
	}
	mock.ExpectRollback()
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTicketCleanupRejectsUserBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cleanup := range []func(*Handler, *gin.Context){(*Handler).deleteTicket, (*Handler).cleanupTickets} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		cleanup(&Handler{}, ctx)
		if response.Code != 404 {
			t.Fatalf("user cleanup returned %d", response.Code)
		}
	}
}
