package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

type recordingSender struct{ delivery domain.Delivery }

func (s *recordingSender) Send(_ context.Context, d domain.Delivery, _, _ string) (string, error) {
	s.delivery = d
	return "ok", nil
}
func TestReadableIdentityAndSecretBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("user", int64(3)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(0, "小明", "a@example.test"))
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("key", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(3, "生产 Key", ""))
	sink := &recordingSender{}
	wrapped := ReadableSender{Next: sink, DB: db}
	d := domain.Delivery{Payload: domain.Incident{Scope: "user:3/key:7"}}
	if _, err = wrapped.Send(context.Background(), d, "", ""); err != nil {
		t.Fatal(err)
	}
	got := sink.delivery.Payload.DisplayScope
	for _, want := range []string{"小明", "a@example.test", "生产 Key", "user:3/key:7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if d.Payload.DisplayScope != "" {
		t.Fatal("original mutated")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReadableIdentityFailureDoesNotBlockAndMismatchDoesNotLie(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("user", int64(3)).WillReturnError(errors.New("offline"))
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("key", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(9, "wrong-owner-key", ""))
	sink := &recordingSender{}
	w := ReadableSender{Next: sink, DB: db}
	if _, err = w.Send(context.Background(), domain.Delivery{Payload: domain.Incident{Scope: "user:3/key:7"}}, "", ""); err != nil {
		t.Fatal(err)
	}
	got := sink.delivery.Payload.DisplayScope
	if strings.Contains(got, "wrong-owner-key") || !strings.Contains(got, "未确认") {
		t.Fatal(got)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReadableNamesCleanAndNoGuessedAggregateUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("group", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(0, "prod\n@all", ""))
	sink := &recordingSender{}
	w := ReadableSender{Next: sink, DB: db}
	if _, err = w.Send(context.Background(), domain.Delivery{Payload: domain.Incident{Scope: "group:2/model:gpt", UserCount: 9}}, "", ""); err != nil {
		t.Fatal(err)
	}
	got := sink.delivery.Payload.DisplayScope
	if strings.Contains(got, "prod\n@all") || strings.Contains(got, "用户：") {
		t.Fatal(got)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadableScopeRenderingAndTransientSerialization(t *testing.T) {
	i := domain.Incident{Scope: "user:3/key:7", DisplayScope: "用户 #3 · 小明 · a@example.test\nKey #7 · <生产> & 业务", Severity: "P3"}
	card, err := Card(i, "")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(card, &decoded); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprint(decoded)
	for _, want := range []string{"小明", "a@example.test", "Key &#35;7", "&lt;生产&gt;"} {
		if !strings.Contains(content, want) {
			t.Fatalf("card missing %q", want)
		}
	}
	if !strings.Contains(Plain(i, ""), "a@example.test") {
		t.Fatal("plain notification missing identity")
	}
	b, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "a@example.test") {
		t.Fatal("transient identity persisted")
	}
}
func TestReadableScopeWithoutDatabaseStillSends(t *testing.T) {
	sink := &recordingSender{}
	w := ReadableSender{Next: sink}
	if _, err := w.Send(context.Background(), domain.Delivery{Payload: domain.Incident{Scope: "user:3/key:7"}}, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.delivery.Payload.DisplayScope, "未确认") {
		t.Fatal("missing unknown label")
	}
}

func TestReadableShowsLatestFaultSampleNotOnlyAggregateScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("user", int64(3)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(0, "小明", "a@example.test"))
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("key", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(3, "团队密钥", ""))
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("account", int64(9)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(0, "上游账号甲", ""))
	mock.ExpectQuery("SELECT owner_id, name, email FROM ops_monitor_v1_identity").WithArgs("group", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"owner_id", "name", "email"}).AddRow(0, "分组甲", ""))
	sink := &recordingSender{}
	w := ReadableSender{Next: sink, DB: db}
	d := domain.Delivery{Payload: domain.Incident{Scope: "channel:1/model:gpt", UserCount: 8, Sample: &domain.IncidentSample{UserID: 3, KeyID: 7, AccountID: 9, GroupID: 2}}}
	if _, err = w.Send(context.Background(), d, "", ""); err != nil {
		t.Fatal(err)
	}
	got := sink.delivery.Payload.DisplayScope
	for _, part := range []string{"最近异常样本", "小明", "a@example.test", "团队密钥", "上游账号甲", "分组甲"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %s", part)
		}
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
