package sources

import (
	"context"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

type Batch struct {
	Observations []domain.Observation
	Cursor       domain.Cursor
	Capabilities []domain.Capability
}
type EvidenceSource interface {
	Name() string
	Poll(context.Context, domain.Cursor, time.Time) (Batch, error)
}

var ErrIncompatible = errors.New("incompatible evidence schema")
var ErrSourceUnavailable = errors.New("evidence source unavailable")
