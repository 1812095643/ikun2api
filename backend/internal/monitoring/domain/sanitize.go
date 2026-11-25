package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func NormalizeIdentifiers(o *Observation) {
	for _, p := range []*string{&o.Site, &o.Instance, &o.Source, &o.EventID, &o.BootID, &o.RequestID, &o.AttemptID, &o.TaskID, &o.Platform, &o.Model} {
		if !safeID.MatchString(*p) || strings.Contains(*p, "://") || strings.Contains(*p, "@") {
			*p = fmt.Sprintf("opaque:%x", sha256.Sum256([]byte(*p)))
		}
	}
}
