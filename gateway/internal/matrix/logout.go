package matrix

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nebu/nebu/internal/middleware"
)

type LogoutHandler struct {
	denylist *middleware.Denylist
}

func NewLogoutHandler(denylist *middleware.Denylist) *LogoutHandler {
	return &LogoutHandler{denylist: denylist}
}

func (h *LogoutHandler) PostLogout(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	expiry, _ := r.Context().Value(middleware.ContextKeyTokenExpiry).(time.Time)
	h.denylist.Add(rawToken, expiry)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct{}{})
}
