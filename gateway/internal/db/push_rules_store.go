package db

// ─── Story 7-30: Push Rules API — PostgreSQL store ───────────────────────────
//
// PostgresPostgresPushRulesDB implements matrix.PushRulesDB using a *sql.DB connection.
// Tables: push_rules (migration 000032).
//
// Default rules are seeded via ON CONFLICT DO NOTHING — idempotent, concurrency-safe.
// Queries use WHERE user_id=$1 directly (no GUC / RLS).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresPushRulesDB implements matrix.PushRulesDB backed by PostgreSQL.
type PostgresPushRulesDB struct {
	db *sql.DB
}

// NewPostgresPushRulesDB constructs a PostgresPushRulesDB from the given *sql.DB.
func NewPostgresPushRulesDB(db *sql.DB) *PostgresPushRulesDB {
	return &PostgresPushRulesDB{db: db}
}

// defaultRules is the ordered list of Matrix-spec default rules (§11.14.1).
// All 15 rules are seeded on first GET /pushrules/ for a user.
var defaultRules = []struct {
	kind   string
	ruleID string
}{
	{"override", "m.rule.master"},
	{"override", "m.rule.suppress_notices"},
	{"override", "m.rule.invite_for_me"},
	{"override", "m.rule.member_event"},
	{"override", "m.rule.is_user_mention"},
	{"override", "m.rule.contains_display_name"},
	{"override", "m.rule.is_room_mention"},
	{"override", "m.rule.tombstone"},
	{"override", "m.rule.roomnotif"},
	{"content", "m.rule.contains_user_name"},
	{"underride", "m.rule.call"},
	{"underride", "m.rule.encrypted_room_one_to_one"},
	{"underride", "m.rule.room_one_to_one"},
	{"underride", "m.rule.message"},
	{"underride", "m.rule.encrypted"},
}

// SeedDefaultRules inserts the 15 Matrix-spec default rules for the given user
// if they do not already exist. Uses ON CONFLICT DO NOTHING for idempotency.
func (p *PostgresPushRulesDB) SeedDefaultRules(ctx context.Context, userID string) error {
	for i, dr := range defaultRules {
		_, err := p.db.ExecContext(ctx,
			`INSERT INTO push_rules
			       (user_id, scope, kind, rule_id, priority, enabled, conditions, actions, default_rule)
			VALUES ($1, 'global', $2, $3, $4, TRUE, '[]', '["notify"]', TRUE)
			ON CONFLICT (user_id, scope, kind, rule_id) DO NOTHING`,
			userID, dr.kind, dr.ruleID, i,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetAllRules returns all push rules for userID in the given scope, ordered by priority ASC.
func (p *PostgresPushRulesDB) GetAllRules(ctx context.Context, userID, scope string) ([]matrix.PushRuleRow, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT user_id, scope, kind, rule_id, priority, enabled, conditions, actions, default_rule
		   FROM push_rules
		  WHERE user_id = $1 AND scope = $2
		  ORDER BY priority ASC, id ASC`,
		userID, scope,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []matrix.PushRuleRow
	for rows.Next() {
		var row matrix.PushRuleRow
		var conditions, actions []byte
		if err := rows.Scan(
			&row.UserID,
			&row.Scope,
			&row.Kind,
			&row.RuleID,
			&row.Priority,
			&row.Enabled,
			&conditions,
			&actions,
			&row.DefaultRule,
		); err != nil {
			return nil, err
		}
		row.Conditions = json.RawMessage(conditions)
		row.Actions = json.RawMessage(actions)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetRule returns a single rule or matrix.ErrPushRuleNotFound.
func (p *PostgresPushRulesDB) GetRule(ctx context.Context, userID, scope, kind, ruleID string) (matrix.PushRuleRow, error) {
	var row matrix.PushRuleRow
	var conditions, actions []byte
	err := p.db.QueryRowContext(ctx,
		`SELECT user_id, scope, kind, rule_id, priority, enabled, conditions, actions, default_rule
		   FROM push_rules
		  WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4`,
		userID, scope, kind, ruleID,
	).Scan(
		&row.UserID,
		&row.Scope,
		&row.Kind,
		&row.RuleID,
		&row.Priority,
		&row.Enabled,
		&conditions,
		&actions,
		&row.DefaultRule,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return matrix.PushRuleRow{}, matrix.ErrPushRuleNotFound
	}
	if err != nil {
		return matrix.PushRuleRow{}, err
	}
	row.Conditions = json.RawMessage(conditions)
	row.Actions = json.RawMessage(actions)
	return row, nil
}

// PutRule creates or replaces a custom rule (upsert by unique key).
// Returns matrix.ErrDefaultRuleImmutable if the rule already exists as a default rule.
// Uses SELECT FOR UPDATE inside a transaction to prevent a concurrent write from
// bypassing the immutability check between the existence check and the upsert.
func (p *PostgresPushRulesDB) PutRule(ctx context.Context, userID string, row matrix.PushRuleRow) error {
	conditions := row.Conditions
	if conditions == nil {
		conditions = json.RawMessage(`[]`)
	}
	actions := row.Actions
	if actions == nil {
		actions = json.RawMessage(`["notify"]`)
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var isDefault bool
	err = tx.QueryRowContext(ctx,
		`SELECT default_rule FROM push_rules
		  WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4
		  FOR UPDATE`,
		userID, row.Scope, row.Kind, row.RuleID,
	).Scan(&isDefault)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if isDefault {
		return matrix.ErrDefaultRuleImmutable
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO push_rules
		       (user_id, scope, kind, rule_id, priority, enabled, conditions, actions, default_rule)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE)
		ON CONFLICT (user_id, scope, kind, rule_id) DO UPDATE
		   SET enabled    = EXCLUDED.enabled,
		       conditions = EXCLUDED.conditions,
		       actions    = EXCLUDED.actions`,
		userID, row.Scope, row.Kind, row.RuleID, row.Priority, row.Enabled,
		[]byte(conditions), []byte(actions),
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteRule removes a custom rule atomically.
// Returns matrix.ErrDefaultRuleImmutable for default rules, matrix.ErrPushRuleNotFound if absent.
func (p *PostgresPushRulesDB) DeleteRule(ctx context.Context, userID, scope, kind, ruleID string) error {
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM push_rules
		  WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4
		    AND NOT default_rule`,
		userID, scope, kind, ruleID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	// Zero rows deleted: either the rule doesn't exist or it's a default rule.
	var exists bool
	err = p.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM push_rules WHERE user_id=$1 AND scope=$2 AND kind=$3 AND rule_id=$4)`,
		userID, scope, kind, ruleID,
	).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return matrix.ErrDefaultRuleImmutable
	}
	return matrix.ErrPushRuleNotFound
}

// SetRuleEnabled updates the enabled flag of any rule (including defaults).
// Returns matrix.ErrPushRuleNotFound if the rule does not exist.
func (p *PostgresPushRulesDB) SetRuleEnabled(ctx context.Context, userID, scope, kind, ruleID string, enabled bool) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE push_rules
		    SET enabled = $5
		  WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4`,
		userID, scope, kind, ruleID, enabled,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return matrix.ErrPushRuleNotFound
	}
	return nil
}

// SetRuleActions replaces the actions array of any rule (including defaults).
// Returns matrix.ErrPushRuleNotFound if the rule does not exist.
func (p *PostgresPushRulesDB) SetRuleActions(ctx context.Context, userID, scope, kind, ruleID string, actions json.RawMessage) error {
	if actions == nil {
		actions = json.RawMessage(`["notify"]`)
	}
	res, err := p.db.ExecContext(ctx,
		`UPDATE push_rules
		    SET actions = $5
		  WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4`,
		userID, scope, kind, ruleID, []byte(actions),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return matrix.ErrPushRuleNotFound
	}
	return nil
}
