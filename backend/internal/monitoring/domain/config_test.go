package domain

import "testing"

func TestDuplicateOwnerSelectorsRejected(t *testing.T) {
	c := DefaultConfig()
	c.OwnerOverrides = []OwnerOverride{{Rule: "U01", Scope: "account:1", Owner: "legacy"}, {Rule: "U01", Scope: "account:1", Owner: "monitor"}}
	if c.Validate() == nil {
		t.Fatal("ambiguous selector accepted")
	}
}
