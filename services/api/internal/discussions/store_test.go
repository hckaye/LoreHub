package discussions

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestTranslateStoreErrorTreatsRestrictViolationAsConflict(t *testing.T) {
	err := translateStoreError("delete discussion category", &pgconn.PgError{Code: "23001"})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("restrict violation = %v, want conflict", err)
	}
}
