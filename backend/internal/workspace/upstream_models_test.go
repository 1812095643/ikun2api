package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/service"
)

func TestUpstreamModelsRejectIncompleteCatalogs(t *testing.T) {
	for _, raw := range []string{`{}`, `{"data":[]}`, `{"data":[{"id":"a"}],"has_more":true}`, `{"data":[{"id":"a"}],"next_page_token":"next"}`, `{"data":[{"id":"a"}],"success":false}`, `{"data":[{"id":"a"}],"error":{"message":"private"}}`, `{"data":[{"id":"bad name"}]}`, `{"data":[{"id":"*"}]}`, `{"data":[{"id":"a"}]}{}`} {
		t.Run(raw, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
			defer server.Close()
			if _, err := queryUpstreamModels(context.Background(), server.Client(), server.URL, "secret"); err == nil {
				t.Fatal("accepted incomplete or invalid catalog")
			}
		})
	}
}
func TestUpstreamModelsReadOnlyAuthAndRedirect(t *testing.T) {
	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer other.Close()
	redirect := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sample" {
			t.Error("wrong request contract")
		}
		if redirect {
			http.Redirect(w, r, other.URL, 302)
			return
		}
		w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-a"}]}`))
	}))
	defer server.Close()
	models, err := queryUpstreamModels(context.Background(), server.Client(), server.URL, "sample")
	if err != nil || !reflect.DeepEqual(models, []string{"model-a", "model-b"}) {
		t.Fatalf("unexpected result %v %v", models, err)
	}
	redirect = true
	if _, err = queryUpstreamModels(context.Background(), server.Client(), server.URL, "sample"); err == nil || reached {
		t.Fatal("redirect followed or accepted")
	}
}
func TestUpstreamModelsBaselineFailuresAndRotation(t *testing.T) {
	a := upstreamBalanceAccount{ID: 1, key: "first", Platform: "openai"}
	now := time.Now()
	state := observeModelCatalog(upstreamModelAccountState{}, a, []string{"a", "b"}, nil, now)
	if d := pendingModelChange(state); len(d.Added)+len(d.Removed) != 0 {
		t.Fatal("first scan should establish baseline")
	}
	state = observeModelCatalog(state, a, []string{"b", "c"}, nil, now)
	if d := pendingModelChange(state); !reflect.DeepEqual(d.Added, []string{"c"}) || !reflect.DeepEqual(d.Removed, []string{"a"}) {
		t.Fatal(d)
	}
	state = observeModelCatalog(state, a, nil, errors.New("unavailable"), now)
	if !reflect.DeepEqual(state.Current, []string{"b", "c"}) || state.Error == "" {
		t.Fatal("failure erased catalog")
	}
	a.key = "rotated"
	state = observeModelCatalog(state, a, []string{"x"}, nil, now)
	if d := pendingModelChange(state); len(d.Added)+len(d.Removed) != 0 {
		t.Fatal("different key inherited prior catalog")
	}
}
func TestUpstreamModelsIgnoreRecoveryAndIsolation(t *testing.T) {
	a := upstreamBalanceAccount{ID: 1, key: "first"}
	now := time.Now()
	state := observeModelCatalog(upstreamModelAccountState{}, a, []string{"a"}, nil, now)
	state = observeModelCatalog(state, a, []string{"a", "b"}, nil, now)
	state.IgnoredAdded = []string{"b"}
	if len(pendingModelChange(state).Added) > 0 {
		t.Fatal("ignored entry still pending")
	}
	state = observeModelCatalog(state, a, []string{"a"}, nil, now)
	state = observeModelCatalog(state, a, []string{"a", "b"}, nil, now)
	if len(pendingModelChange(state).Added) != 1 {
		t.Fatal("new reappearance not detected")
	}
	pub := publicModelRecord(upstreamModelRecord{Accounts: []upstreamModelAccountState{state}}, upstreamBalanceGroup{Accounts: []upstreamBalanceAccount{a}})
	raw, _ := json.Marshal(pub)
	if strings.Contains(string(raw), "source") || strings.Contains(string(raw), "first") {
		t.Fatal("source identity exposed")
	}
}
func TestUpstreamModelsApplyPreservesAliasesAndNoBroadening(t *testing.T) {
	s := upstreamModelAccountState{Baseline: []string{"a", "b"}}
	change := upstreamModelChange{Added: []string{"c"}, Removed: []string{"a"}}
	mapping, err := applyModelMapping(map[string]any{"a": "a", "b": "b", "custom": "b"}, s, change, false)
	if err != nil || mapping["custom"] != "b" || mapping["b"] != "b" || mapping["c"] != "c" {
		t.Fatalf("%v %v", mapping, err)
	}
	if _, ok := mapping["a"]; ok {
		t.Fatal("removal not applied")
	}
	cases := []struct {
		raw        any
		change     upstreamModelChange
		initialize bool
	}{
		{nil, change, false},
		{map[string]any{"a": "b"}, sChange("a"), false},
		{map[string]any{"alias": "a", "a": "a"}, sChange("a"), false},
		{map[string]any{"*": "*", "a": "a"}, sChange("a"), false},
		{map[string]any{"a": "a"}, sChange("a"), false},
	}
	for _, tc := range cases {
		if _, err = applyModelMapping(tc.raw, s, tc.change, tc.initialize); err == nil {
			t.Fatal("unsafe mapping accepted")
		}
	}
	if _, err = applyModelMapping(nil, s, change, true); err != nil {
		t.Fatal(err)
	}
}
func sChange(model string) upstreamModelChange { return upstreamModelChange{Removed: []string{model}} }
func TestUpstreamModelsActionValidatesSelectedSnapshot(t *testing.T) {
	now := time.Now()
	s := upstreamModelAccountState{Baseline: []string{"a"}, Current: []string{"b"}, CheckedAt: &now}
	if err := validateModelAction(upstreamModelAction{Action: "apply", Added: []string{"forged"}}, s); err == nil {
		t.Fatal("forged model accepted")
	}
	s.Error = "timeout"
	if err := validateModelAction(upstreamModelAction{Action: "apply", Added: []string{"b"}}, s); err == nil {
		t.Fatal("failed catalog applied")
	}
	if err := validateModelAction(upstreamModelAction{Action: "ignore", Added: []string{"b"}}, s); err != nil {
		t.Fatal(err)
	}
}
func modelMockRows(state upstreamModelAccountState) *sqlmock.Rows {
	raw, _ := json.Marshal([]upstreamModelAccountState{state})
	return sqlmock.NewRows(strings.Split(modelRecordColumns, ",")).AddRow(`{"interval_minutes":30}`, string(raw), "[]", "{}", 2, false, nil, time.Now(), time.Now())
}
func TestUpstreamModelsAtomicMappingAndOutbox(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "outbox_failure"}[rollback], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			h := &Handler{db: db}
			now := time.Now()
			a := upstreamBalanceAccount{ID: 4, Platform: "openai", key: "secret"}
			s := upstreamModelAccountState{AccountID: 4, Source: modelSource(a), Baseline: []string{"a"}, Current: []string{"a", "b"}, CheckedAt: &now}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM workspace_upstream_models .*FOR UPDATE").WithArgs(upstreamBalanceID("https://up.example")).WillReturnRows(modelMockRows(s))
			mock.ExpectQuery("SELECT id,name,platform,type,credentials,proxy_id FROM accounts").WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "type", "credentials", "proxy_id"}).AddRow(4, "up", "openai", "apikey", `{"api_key":"secret","base_url":"https://up.example","model_mapping":{"a":"a"},"custom_setting":true}`, nil))
			mock.ExpectExec("UPDATE accounts SET credentials=jsonb_set").WithArgs(int64(4), `{"a":"a","b":"b"}`).WillReturnResult(sqlmock.NewResult(0, 1))
			out := mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(4))
			if rollback {
				out.WillReturnError(errors.New("outbox unavailable"))
				mock.ExpectRollback()
			} else {
				out.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE workspace_upstream_models SET state=").WithArgs(upstreamBalanceID("https://up.example"), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err = h.applyUpstreamModelAction(context.Background(), upstreamBalanceID("https://up.example"), 1, upstreamModelAction{Revision: 2, AccountID: 4, Action: "apply", Added: []string{"b"}})
			if (err != nil) != rollback {
				t.Fatalf("err=%v rollback=%v", err, rollback)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestUpstreamModelsStaleRevisionRejected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*FOR UPDATE").WithArgs("site").WillReturnRows(modelMockRows(upstreamModelAccountState{}))
	mock.ExpectRollback()
	err := (&Handler{db: db}).applyUpstreamModelAction(context.Background(), "site", 1, upstreamModelAction{Revision: 1, AccountID: 4, Action: "apply", Added: []string{"b"}})
	var e *requestError
	if !errors.As(err, &e) || e.status != 409 {
		t.Fatalf("wrong error %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamModelsNotificationDedupAcrossPartialActions(t *testing.T) {
	r := upstreamModelRecord{Config: upstreamModelConfig{Recipients: []string{"ops@example.com"}}, Accounts: []upstreamModelAccountState{{AccountID: 1, Source: "one", Baseline: []string{"a"}, Current: []string{"a", "b", "c"}}}}
	notice, to, keys := modelNotificationPlan(r)
	if len(to) != 1 || len(keys) != 2 {
		t.Fatal("new events not selected")
	}
	notice.Delivered[to[0]] = keys
	r.notice = notice
	r.Accounts[0].IgnoredAdded = []string{"b"}
	_, to, _ = modelNotificationPlan(r)
	if len(to) != 0 {
		t.Fatal("partial acknowledgement resent existing change")
	}
	r.Accounts[0].Current = []string{"a"}
	r.notice, _, _ = modelNotificationPlan(r)
	r.Accounts[0].Current = []string{"a", "c"}
	_, to, _ = modelNotificationPlan(r)
	if len(to) != 1 {
		t.Fatal("reappearing event not notified")
	}
}
