package http

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sbezhuk/beebase-common/httpx"
	"net/http"
	"time"
)

type statusResponse struct {
	Status string `json:"status"`
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}
func ReadyHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	}
}
