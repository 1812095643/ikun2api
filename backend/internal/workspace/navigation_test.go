package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestNavigationReadCannotClearBusinessState(t *testing.T) {
	for _, section := range []string{"tickets", "checkin", "announcements", "pending_wins", "admin_lottery", "unknown"} {
		if validateNavigationRead(navigationReadPayload{Section: section, Keys: []string{"1"}}, false) == nil {
			t.Fatalf("accepted restricted section %s", section)
		}
	}
	for _, keys := range [][]string{nil, {""}, {"\x00"}, {"1\n"}, make([]string, 101)} {
		if validateNavigationRead(navigationReadPayload{Section: "news", Keys: keys}, false) == nil {
			t.Fatal("accepted invalid keys")
		}
	}
	if validateNavigationRead(navigationReadPayload{Section: "admin_lottery", Keys: []string{"open:1"}}, true) != nil {
		t.Fatal("rejected admin")
	}
}

func TestNavigationReadMergesRecentKeysWithinBound(t *testing.T) {
	keys := []string{}
	for i := 0; i < 4100; i++ {
		keys = append(keys, fmt.Sprint(i))
	}
	result := mergeNavigationKeys(keys, []string{"5", "4099", "new"})
	if len(result) != 4096 || result[len(result)-1] != "new" || result[len(result)-3] != "5" {
		t.Fatal("recent bounded history changed")
	}
	seen := map[string]bool{}
	for _, key := range result {
		if seen[key] {
			t.Fatal("duplicate key")
		}
		seen[key] = true
	}
}

func TestNavigationFeedKeysUseActualIDs(t *testing.T) {
	for _, raw := range []string{`{"items":[{"id":2},{"id":9007199254740993},{"id":-1}]}`, `{"code":0,"data":{"items":[{"id":2},{"id":9007199254740993}]}}`} {
		if !reflect.DeepEqual(navigationFeedKeys([]byte(raw)), []string{"2", "9007199254740993"}) {
			t.Fatal("feed identifiers changed")
		}
	}
	if len(navigationFeedKeys([]byte(`{"message":"unavailable"}`))) != 0 {
		t.Fatal("invented feed content")
	}
}

func TestNavigationReadTransactionScopesUserAndRollsBack(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO workspace_navigation_reads").WithArgs(int64(8), "news").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT seen_keys FROM workspace_navigation_reads WHERE user_id=\\$1 AND section=\\$2 FOR UPDATE").WithArgs(int64(8), "news").WillReturnRows(sqlmock.NewRows([]string{"keys"}).AddRow(`["1"]`))
		query := mock.ExpectExec("UPDATE workspace_navigation_reads.*WHERE user_id=\\$1 AND section=\\$2").WithArgs(int64(8), "news", `["1","2"]`)
		if writeFails {
			query.WillReturnError(errors.New("write unavailable"))
			mock.ExpectRollback()
		} else {
			query.WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		h := &Handler{db: db}
		keys, err := h.saveNavigationRead(context.Background(), 8, navigationReadPayload{Section: "news", Keys: []string{"2"}})
		if (err != nil) != writeFails {
			t.Fatalf("unexpected transaction result %v", err)
		}
		if !writeFails && !reflect.DeepEqual(keys, []string{"1", "2"}) {
			t.Fatal("read history lost")
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestNavigationRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, handler := range []func(*Handler, *gin.Context){(*Handler).navigation, (*Handler).readNavigation} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest("GET", "/", nil)
		handler(&Handler{}, c)
		var body map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &body)
		if r.Code != 401 {
			t.Fatalf("anonymous status %d %v", r.Code, body)
		}
	}
}
