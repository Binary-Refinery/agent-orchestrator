package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrIncompatibleMigrationLineage means the database belongs to an unmerged
// upstream lineage (or has lost a required fork schema). Numerical Goose
// versions alone cannot establish compatibility across the 165–170 collision.
var ErrIncompatibleMigrationLineage = errors.New("incompatible database migration lineage")

func validateForkMigrationLineage(db *sql.DB) error {
	var hasLedger int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&hasLedger); err != nil {
		return fmt.Errorf("inspect migration lineage: %w", err)
	}
	if hasLedger == 0 {
		return nil
	}
	// Validate only applied fork versions, allowing both fresh databases and
	// upgrades from the common pre-165 schema. This also refuses upstream at
	// 165, before its max version grows beyond this binary's version.
	checks := []struct {
		version int
		query   string
	}{
		{165, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='instructor_works'`},
		{166, `SELECT COUNT(*) FROM pragma_table_info('review') WHERE name='launch_agent_config'`},
		{167, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='semantic_deliveries'`},
		{168, `SELECT COUNT(*) FROM pragma_table_info('review_run') WHERE name='scope_key'`},
		{169, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='instructor_plans'`},
		{170, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='instructor_inventory_cache'`},
	}
	for _, check := range checks {
		var applied, present int
		if err := db.QueryRow(`SELECT COALESCE((SELECT is_applied FROM goose_db_version WHERE version_id=? ORDER BY id DESC LIMIT 1),0)`, check.version).Scan(&applied); err != nil {
			return fmt.Errorf("inspect migration %d lineage: %w", check.version, err)
		}
		if applied != 1 {
			continue
		}
		if err := db.QueryRow(check.query).Scan(&present); err != nil {
			return fmt.Errorf("inspect migration %d schema: %w", check.version, err)
		}
		if present == 0 {
			return fmt.Errorf("%w: applied migration %d lacks its fork schema; upstream and fork migrations 165–170 differ. No schema or migration-history changes were made. Keep the original database and use its matching build or a separately validated lineage merge; use a separate AO_DATA_DIR for this fork", ErrIncompatibleMigrationLineage, check.version)
		}
	}
	return nil
}
