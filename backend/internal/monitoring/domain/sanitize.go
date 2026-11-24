package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// NormalizeIdentifiers preserves correlation but never exports URL-shaped or
// free-form identifiers. Transport/parser bodies have no field in this DTO.
func NormalizeIdentifiers(o *Observation) {
	for _, p := range []*string{&o.Site, &o.Instance, &o.Source, &o.EventID, &o.BootID, &o.RequestID, &o.AttemptID, &o.TaskID, &o.Platform, &o.Model} {
		if !safeID.MatchString(*p) || strings.Contains(*p, "://") || strings.Contains(*p, "@") {
			*p = fmt.Sprintf("opaque:%x", sha256.Sum256([]byte(*p)))
		}
	}
}
