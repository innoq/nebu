package db

import (
	"context"
	"database/sql"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresUserDirectoryDB implements matrix.UserDirectoryDB using a PostgreSQL connection.
// The SQL query uses the ESCAPE clause so that LIKE metacharacters in the pattern
// (already escaped by matrix.EscapeLIKE) are treated as literals.
type PostgresUserDirectoryDB struct {
	db *sql.DB
}

// NewPostgresUserDirectoryDB constructs a PostgresUserDirectoryDB backed by the given *sql.DB.
func NewPostgresUserDirectoryDB(db *sql.DB) *PostgresUserDirectoryDB {
	return &PostgresUserDirectoryDB{db: db}
}

// SearchUsers executes a case-insensitive user_id search with ESCAPE '\'.
// The caller must pass an already-escaped LIKE pattern (wrapped in %…%).
// display_name is read from profiles.displayname (nullable → COALESCE to '').
func (p *PostgresUserDirectoryDB) SearchUsers(ctx context.Context, pattern string, limit int) ([]matrix.UserDirectoryResult, error) {
	rows, err := p.db.QueryContext(ctx,
		// ESCAPE '\' ensures that \%, \_, \\ in the pattern are treated as literals.
		// LEFT JOIN profiles so users without a profile row still appear.
		`SELECT u.user_id, COALESCE(p.displayname, '')
		 FROM users u
		 LEFT JOIN profiles p ON p.user_id = u.user_id
		 WHERE u.user_id ILIKE $1 ESCAPE '\'
		 LIMIT $2`,
		pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []matrix.UserDirectoryResult
	for rows.Next() {
		var r matrix.UserDirectoryResult
		if err := rows.Scan(&r.UserID, &r.DisplayName); err != nil {
			continue
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
