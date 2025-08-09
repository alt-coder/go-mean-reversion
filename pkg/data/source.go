package data

import (
	"context"

	"github.com/alt-coder/go-mean-reversion/pkg/models"
)

// Source provides market data for candidate selection.
type Source interface {
	// TopCandidates returns a list of potential trades sorted by desirability.
	TopCandidates(ctx context.Context) ([]models.Candidate, error)
}
