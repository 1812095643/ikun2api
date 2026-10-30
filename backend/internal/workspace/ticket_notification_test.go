package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestTicketNotificationDeniesNonAdminBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	(&Handler{}).createTicketNotification(c)
	if r.Code != 404 {
		t.Fatalf("unauthorized status %d", r.Code)
	}
}

func notificationPayload() ticketNotificationPayload {
	return ticketNotificationPayload{UserID: 8, RequestKey: "notification-request-0001", ticketPayload: ticketPayload{Subject: "请检查请求地址", Category: "general", Priority: "normal", Content: "请将地址改为 /v1/responses"}}
}

func TestTicketNotificationRejectsTamperedFieldsAndEmptyMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := `{"user_id":8,"request_key":"notification-request-0001","subject":"通知","content":"检查接口"}`
	for _, raw := range []string{
		strings.Replace(valid, `"user_id":8`, `"user_id":0`, 1), strings.Replace(valid, `"content":"检查接口"`, `"content":"  "`, 1),
		strings.Replace(valid, `"subject":"通知"`, `"subject":""`, 1), strings.Replace(valid, `"notification-request-0001"`, `"x"`, 1),
		strings.Replace(valid, `"content":"检查接口"`, `"author_role":"user","content":"检查接口"`, 1), valid + `{}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(raw))
		if _, _, err := decodeTicketNotification(c); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(valid))
	if p, _, err := decodeTicketNotification(c); err != nil || p.UserID != 8 || p.Category != "general" {
		t.Fatalf("valid notification %v", err)
	}
}

func notificationHash(p ticketNotificationPayload) string {
	raw, _ := json.Marshal(struct {
		UserID  int64
		Payload ticketPayload
	}{p.UserID, p.ticketPayload})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func TestTicketNotificationAtomicCreationAndIdempotency(t *testing.T) {
	for _, scenario := range []string{"created", "retry", "changed", "deleted", "message_failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			p := notificationPayload()
			hash := notificationHash(p)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT email,username FROM users.*FOR SHARE").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"email", "username"}).AddRow("owner@example.test", "owner"))
			inserted := int64(1)
			if scenario == "retry" || scenario == "changed" || scenario == "deleted" {
				inserted = 0
			}
			mock.ExpectExec("INSERT INTO workspace_ticket_notifications").WithArgs(int64(1), p.RequestKey, int64(8), hash).WillReturnResult(sqlmock.NewResult(0, inserted))
			storedHash := hash
			if scenario == "changed" {
				storedHash = "different"
			}
			var prior any
			if scenario == "retry" || scenario == "changed" {
				prior = int64(42)
			}
			mock.ExpectQuery("SELECT user_id,payload_hash,ticket_id.*FOR UPDATE").WithArgs(int64(1), p.RequestKey).WillReturnRows(sqlmock.NewRows([]string{"user_id", "hash", "ticket_id"}).AddRow(8, storedHash, prior))
			if inserted == 1 {
				mock.ExpectQuery("INSERT INTO workspace_tickets.*").WithArgs(int64(8), "owner@example.test", "owner", p.Subject, "general", "normal").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				message := mock.ExpectQuery("INSERT INTO workspace_ticket_messages").WithArgs(int64(42), int64(1), "支持团队", p.Content)
				if scenario == "message_failure" {
					message.WillReturnError(errors.New("storage unavailable"))
				} else {
					message.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
					mock.ExpectExec("UPDATE workspace_ticket_notifications SET ticket_id").WithArgs(int64(1), p.RequestKey, int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			wantError := scenario == "changed" || scenario == "deleted" || scenario == "message_failure"
			if wantError {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			id, _, duplicate, err := (&Handler{db: db}).storeTicketNotification(context.Background(), actor{ID: 1, Admin: true, Name: "支持团队"}, p, nil)
			if (err != nil) != wantError {
				t.Fatalf("unexpected error %v", err)
			}
			if !wantError && (id != 42 || duplicate != (scenario == "retry")) {
				t.Fatalf("unexpected outcome %d %v", id, duplicate)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTicketNotificationRejectsUnavailableRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT email,username FROM users.*FOR SHARE").WithArgs(int64(8)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, _, _, err = (&Handler{db: db}).storeTicketNotification(context.Background(), actor{ID: 1, Admin: true}, notificationPayload(), nil)
	var rejection *requestError
	if !errors.As(err, &rejection) || rejection.status != 400 {
		t.Fatalf("unavailable recipient %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
