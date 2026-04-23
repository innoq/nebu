package matrix

// Story 5-26: POST /_matrix/client/v3/user_directory/search
//
// Fixes:
//  1. Input validation: trim whitespace, reject empty/too-short/too-long search terms.
//  2. LIKE-metachar escaping: \, %, _ escaped before wrapping in %…% wildcard.
//  3. SQL ESCAPE clause: WHERE display_name ILIKE $1 ESCAPE '\'.
//  4. Panic-fix: guard against uid without ':' before slicing.
//  5. Result cap: limit > 100 → 100; limit == 0 → default 10.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// UserDirectoryDB is the consumer-defined interface for user directory searches.
// Implementations must execute:
//
//	SELECT user_id, display_name FROM users
//	WHERE display_name ILIKE $1 ESCAPE '\'
//	LIMIT $2
//
// The caller passes an already-escaped LIKE pattern (wrapped in %…%).
type UserDirectoryDB interface {
	SearchUsers(ctx context.Context, pattern string, limit int) ([]UserDirectoryResult, error)
}

// UserDirectoryResult holds a single row returned from UserDirectoryDB.SearchUsers.
type UserDirectoryResult struct {
	UserID      string
	DisplayName string
}

// UserDirectoryConfig holds dependencies for NewUserDirectoryHandler.
type UserDirectoryConfig struct {
	DB         UserDirectoryDB
	ServerName string
}

// UserDirectoryHandler handles POST /_matrix/client/v3/user_directory/search.
type UserDirectoryHandler struct {
	db         UserDirectoryDB
	serverName string
}

// NewUserDirectoryHandler constructs a UserDirectoryHandler from the provided config.
func NewUserDirectoryHandler(cfg UserDirectoryConfig) *UserDirectoryHandler {
	return &UserDirectoryHandler{
		db:         cfg.DB,
		serverName: cfg.ServerName,
	}
}

// likeEscaper escapes LIKE metacharacters in order:
//  1. Backslash first — prevents double-escaping newly inserted escape chars.
//  2. Percent sign.
//  3. Underscore.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLIKE returns s with LIKE metacharacters escaped for use with ESCAPE '\'.
// The escape order is critical: backslash must be processed first.
func EscapeLIKE(s string) string {
	return likeEscaper.Replace(s)
}

// Search handles POST /_matrix/client/v3/user_directory/search.
//
// Flow:
//  1. Decode JSON body.
//  2. Validate and normalise SearchTerm (trim, length 2–64).
//  3. Apply result-count limits (default 10, max 100).
//  4. Escape LIKE metacharacters and wrap in %…% wildcard.
//  5. Query UserDirectoryDB.
//  6. Build the Matrix-spec response (results + limited).
func (h *UserDirectoryHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SearchTerm string `json:"search_term"`
		Limit      int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Request body is not valid JSON")
		return
	}

	// AC #1 — Trim and validate SearchTerm.
	term := strings.TrimSpace(req.SearchTerm)
	if len(term) == 0 {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "search_term must not be empty")
		return
	}
	if len([]rune(term)) < 2 {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "search_term must be at least 2 characters")
		return
	}
	if len([]rune(term)) > 64 {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "search_term must not exceed 64 characters")
		return
	}

	// AC #5 — Clamp limit.
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}

	// AC #2 — Escape LIKE metacharacters, then wrap in %…%.
	pattern := "%" + EscapeLIKE(term) + "%"

	// AC #3 — Query with ESCAPE clause (delegated to DB implementation).
	rows, err := h.db.SearchUsers(r.Context(), pattern, limit)
	if err != nil {
		slog.Error("user_directory search failed", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[],"limited":false}`))
		return
	}

	// Build response.
	type resultItem struct {
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
	}
	results := make([]resultItem, 0, len(rows))
	for _, row := range rows {
		uid := row.UserID

		// AC #4 — Panic-fix: guard against uid without ':'.
		i := strings.IndexByte(uid, ':')
		if i <= 0 {
			continue
		}

		displayName := row.DisplayName
		if displayName == "" {
			// Derive display name from the localpart if the DB column is empty.
			displayName = uid[1:i]
		}
		results = append(results, resultItem{
			UserID:      uid,
			DisplayName: displayName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"limited": false,
	})
}
