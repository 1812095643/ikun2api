package domain

import "time"

const (
	QuietRequest = 2 * time.Hour
	QuietState   = 24 * time.Hour
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

func QuietCloseAfter(rule string) time.Duration { return quietClose[rule] }

func QuietClosedText(rule string) string {
	if QuietCloseAfter(rule) >= QuietState {
		return "24 小时没有新进展，已自动关闭"
	}
	return "2 小时无新报错，已自动关闭"
}
