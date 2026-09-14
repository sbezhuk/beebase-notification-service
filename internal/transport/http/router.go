package http

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sbezhuk/beebase-common/authmw"
	"github.com/sbezhuk/beebase-common/httpx"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"log/slog"
	"net/http"
	"time"
)

type Handler struct {
	service *appnotification.Service
	log     *slog.Logger
}

func NewHandler(s *appnotification.Service, log *slog.Logger) *Handler {
	return &Handler{service: s, log: log}
}

type registerRequest struct {
	Destination string              `json:"destination"`
	Platform    pushdevice.Platform `json:"platform"`
}
type sendRequest struct {
	Destination, Title, Body string
	Data                     map[string]string `json:"data"`
}

func NewRouter(log *slog.Logger, db *pgxpool.Pool, h *Handler, parser authmw.AccessTokenParser) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Get("/health", HealthHandler)
	r.Get("/ready", ReadyHandler(db))
	r.Route("/api/v1/devices", func(r chi.Router) {
		r.Use(authmw.RequireAuth(parser))
		r.Post("/", h.Register)
		r.Delete("/{deviceID}", h.Remove)
	})
	r.Route("/api/v1/notifications", func(r chi.Router) { r.Use(authmw.RequireAuth(parser)); r.Post("/test", h.SendTest) })
	return r
}
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "invalid_request", "invalid JSON")
		return
	}
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	d, err := h.service.RegisterDevice(r.Context(), uid, req.Destination, req.Platform)
	if err != nil {
		httpx.WriteError(w, 400, "invalid_device", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		httpx.WriteError(w, 400, "invalid_device_id", "invalid device id")
		return
	}
	if err = h.service.RemoveDevice(r.Context(), id, uid); err != nil {
		if errors.Is(err, pushdevice.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		httpx.WriteError(w, 500, "remove_failed", "could not remove device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) SendTest(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "invalid_request", "invalid JSON")
		return
	}
	if err := h.service.Send(r.Context(), appnotification.PushMessage{Destination: req.Destination, Title: req.Title, Body: req.Body, Data: req.Data}); err != nil {
		var deliveryErr *appnotification.DeliveryError
		if errors.As(err, &deliveryErr) {
			switch deliveryErr.Kind {
			case appnotification.DeliveryInvalidDestination:
				httpx.WriteError(w, http.StatusBadRequest, string(deliveryErr.Kind), "invalid notification destination")
				return
			case appnotification.DeliveryUnregistered:
				httpx.WriteError(w, http.StatusGone, string(deliveryErr.Kind), "notification destination is no longer registered")
				return
			case appnotification.DeliveryAuthentication:
				httpx.WriteError(w, http.StatusServiceUnavailable, string(deliveryErr.Kind), "notification provider authentication failed")
				return
			case appnotification.DeliveryTemporary:
				httpx.WriteError(w, http.StatusServiceUnavailable, string(deliveryErr.Kind), "notification provider is temporarily unavailable")
				return
			}
		}
		httpx.WriteError(w, http.StatusBadGateway, "send_failed", "could not send notification")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, statusResponse{Status: "accepted"})
}
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
		})
	}
}
