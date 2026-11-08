package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJavaScriptUTF16PrefixMatchesNodeReference(t *testing.T) {
	// Fixed results from Node.js String.slice with a trailing high surrogate
	// removed. Keep the reference independent of the Go implementation.
	for _, tc := range []struct {
		text  string
		limit int
		want  string
	}{
		{text: "", limit: 0, want: ""},
		{text: "", limit: 1, want: ""},
		{text: "ASCII", limit: 0, want: ""},
		{text: "ASCII", limit: 1, want: "A"},
		{text: "ASCII", limit: 2, want: "AS"},
		{text: "ASCII", limit: 3, want: "ASC"},
		{text: "ASCII", limit: 4, want: "ASCI"},
		{text: "ASCII", limit: 5, want: "ASCII"},
		{text: "ASCII", limit: 6, want: "ASCII"},
		{text: "中文", limit: 0, want: ""},
		{text: "中文", limit: 1, want: "中"},
		{text: "中文", limit: 2, want: "中文"},
		{text: "中文", limit: 3, want: "中文"},
		{text: "A中B", limit: 0, want: ""},
		{text: "A中B", limit: 1, want: "A"},
		{text: "A中B", limit: 2, want: "A中"},
		{text: "A中B", limit: 3, want: "A中B"},
		{text: "A中B", limit: 4, want: "A中B"},
		{text: "A🚀B", limit: 0, want: ""},
		{text: "A🚀B", limit: 1, want: "A"},
		{text: "A🚀B", limit: 2, want: "A"},
		{text: "A🚀B", limit: 3, want: "A🚀"},
		{text: "A🚀B", limit: 4, want: "A🚀B"},
		{text: "A🚀B", limit: 5, want: "A🚀B"},
		{text: "🚀🚀尾", limit: 0, want: ""},
		{text: "🚀🚀尾", limit: 1, want: ""},
		{text: "🚀🚀尾", limit: 2, want: "🚀"},
		{text: "🚀🚀尾", limit: 3, want: "🚀"},
		{text: "🚀🚀尾", limit: 4, want: "🚀🚀"},
		{text: "🚀🚀尾", limit: 5, want: "🚀🚀尾"},
		{text: "🚀🚀尾", limit: 6, want: "🚀🚀尾"},
		{text: "𠀀中", limit: 0, want: ""},
		{text: "𠀀中", limit: 1, want: ""},
		{text: "𠀀中", limit: 2, want: "𠀀"},
		{text: "𠀀中", limit: 3, want: "𠀀中"},
		{text: "𠀀中", limit: 4, want: "𠀀中"},
		{text: "é中", limit: 0, want: ""},
		{text: "é中", limit: 1, want: "e"},
		{text: "é中", limit: 2, want: "é"},
		{text: "é中", limit: 3, want: "é中"},
		{text: "é中", limit: 4, want: "é中"},
		{text: "中\u0000文", limit: 0, want: ""},
		{text: "中\u0000文", limit: 1, want: "中"},
		{text: "中\u0000文", limit: 2, want: "中\u0000"},
		{text: "中\u0000文", limit: 3, want: "中\u0000文"},
		{text: "中\u0000文", limit: 4, want: "中\u0000文"},
		{text: "\\u4e2d\\u6587", limit: 0, want: ""},
		{text: "\\u4e2d\\u6587", limit: 1, want: "\\"},
		{text: "\\u4e2d\\u6587", limit: 2, want: "\\u"},
		{text: "\\u4e2d\\u6587", limit: 3, want: "\\u4"},
		{text: "\\u4e2d\\u6587", limit: 4, want: "\\u4e"},
		{text: "\\u4e2d\\u6587", limit: 5, want: "\\u4e2"},
		{text: "\\u4e2d\\u6587", limit: 6, want: "\\u4e2d"},
		{text: "\\u4e2d\\u6587", limit: 7, want: "\\u4e2d\\"},
		{text: "\\u4e2d\\u6587", limit: 8, want: "\\u4e2d\\u"},
		{text: "\\u4e2d\\u6587", limit: 9, want: "\\u4e2d\\u6"},
		{text: "\\u4e2d\\u6587", limit: 10, want: "\\u4e2d\\u65"},
		{text: "\\u4e2d\\u6587", limit: 11, want: "\\u4e2d\\u658"},
		{text: "\\u4e2d\\u6587", limit: 12, want: "\\u4e2d\\u6587"},
		{text: "\\u4e2d\\u6587", limit: 13, want: "\\u4e2d\\u6587"},
	} {
		t.Run(fmt.Sprintf("%q/limit=%d", tc.text, tc.limit), func(t *testing.T) {
			got, truncated := javascriptUTF16Prefix(tc.text, tc.limit)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.want != tc.text, truncated)
		})
	}
}
