package workspace

import (
	"strings"
	"testing"
)

func TestLotteryHistoryAlwaysScopesPersonalRequests(t *testing.T) {
	where, args, err := lotteryHistoryScope(actor{ID: 27}, lotteryHistoryQuery{LotteryID: 3, Activity: "' OR TRUE --", Search: "another@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "e.user_id=$1") || len(args) != 3 || args[0] != int64(27) {
		t.Fatalf("owner scope missing: %s %#v", where, args)
	}
	if strings.Contains(where, "another@example.com") || strings.Contains(where, "' OR TRUE --") {
		t.Fatal("request text interpolated into SQL")
	}
	if strings.Contains(where, "u.email") {
		t.Fatal("personal request accepted administrator user search")
	}
	if _, _, err := lotteryHistoryScope(actor{}, lotteryHistoryQuery{}); err == nil {
		t.Fatal("unauthenticated scope accepted")
	}
}

func TestLotteryHistoryAdminScopeAndResultWhitelist(t *testing.T) {
	where, args, err := lotteryHistoryScope(actor{ID: 1, Admin: true}, lotteryHistoryQuery{Search: "27"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(where, "e.user_id=$") || !strings.Contains(where, "u.email") || len(args) != 1 {
		t.Fatal("admin scope is incorrect")
	}
	for _, result := range []string{"", "pending", "won", "lost", "cancelled", "credited", "unclaimed"} {
		if _, err := lotteryHistoryResultFilter(result); err != nil {
			t.Fatalf("rejected %s", result)
		}
	}
	for _, result := range []string{"all_users", "won' OR TRUE", "deleted"} {
		if _, err := lotteryHistoryResultFilter(result); err == nil {
			t.Fatalf("accepted %q", result)
		}
	}
}
