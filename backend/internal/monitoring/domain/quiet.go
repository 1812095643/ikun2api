package domain

import "time"

// Incidents close themselves after a stretch with no new fault. Faults are
// time-sensitive: one that is still happening keeps producing fresh evidence.
const (
	// QuietRequest covers rules whose evidence only arrives with traffic, or from
	// a periodic snapshot that re-reports a standing fault.
	QuietRequest = 2 * time.Hour
	// QuietState covers states that can persist without new events (quarantine,
	// pool shrink, tasks, host and monitor health). A day without any new event
	// means nobody is acting on it here; the account page still shows the state.
	QuietState = 24 * time.Hour
)

var quietClose = map[string]time.Duration{
	"Q01": QuietRequest, "Q02": QuietRequest, "Q05": QuietRequest,
	"U01": QuietRequest, "U02": QuietRequest, "U03": QuietRequest,
	"S01": QuietRequest,
	"L01": QuietRequest, "L02": QuietRequest, "L03": QuietRequest, "L04": QuietRequest,
	"F01": QuietRequest, "F02": QuietRequest,
	"R01": QuietRequest, "R02": QuietRequest,
	"C01": QuietRequest,
	"S02": QuietState, "S03": QuietState,
	"A01": QuietState,
	"H01": QuietState, "H02": QuietState,
	"M01": QuietState, "M02": QuietState,
}

// QuietCloseAfter returns how long a rule's incident may stay silent before it
// closes itself, or 0 when it needs explicit recovery evidence. Money rules
// (paid orders, billing, reconciliation) never close on silence.
func QuietCloseAfter(rule string) time.Duration { return quietClose[rule] }

// QuietClosedText describes a quiet close for people.
func QuietClosedText(rule string) string {
	if QuietCloseAfter(rule) >= QuietState {
		return "24 小时没有新进展，已自动关闭"
	}
	return "2 小时无新报错，已自动关闭"
}
