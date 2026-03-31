package matrix

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nebu/nebu/internal/middleware"
)

type LogoutHandler struct {
	store middleware.TokenStore
}

func NewLogoutHandler(store middleware.TokenStore) *LogoutHandler {
	return &LogoutHandler{store: store}
}

func (h *LogoutHandler) PostLogout(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	expiry, _ := r.Context().Value(middleware.ContextKeyTokenExpiry).(time.Time)
	_ = h.store.Invalidate(rawToken, expiry)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct{}{})
}
