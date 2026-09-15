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
	"github.com/sbezhuk/beebase-common/internalauth"
	"github.com/sbezhuk/beebase-common/pagination"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
	"log/slog"
	"net/http"
	"time"
)

type Handler struct {
	service   *appnotification.Service
	reminders *appnotification.ReminderService
	log       *slog.Logger
}

func NewHandler(s *appnotification.Service, log *slog.Logger, rs ...*appnotification.ReminderService) *Handler {
	h := &Handler{service: s, log: log}
	if len(rs) > 0 {
		h.reminders = rs[0]
	}
	return h
}

type registerRequest struct {
	Destination string              `json:"destination"`
	Platform    pushdevice.Platform `json:"platform"`
}

type pushDeviceResponse struct {
	ID          uuid.UUID           `json:"id"`
	UserID      uuid.UUID           `json:"user_id"`
	Destination string              `json:"destination"`
	Platform    pushdevice.Platform `json:"platform"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

func newPushDeviceResponse(d *pushdevice.PushDevice) pushDeviceResponse {
	return pushDeviceResponse{
		ID:          d.ID,
		UserID:      d.UserID,
		Destination: d.Destination,
		Platform:    d.Platform,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func NewRouter(log *slog.Logger, db *pgxpool.Pool, h *Handler, parser authmw.AccessTokenParser, internalTokens ...string) http.Handler {
	internalToken := ""
	if len(internalTokens) > 0 {
		internalToken = internalTokens[0]
	}
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
		r.Put("/{deviceID}", h.Update)
		r.Delete("/{deviceID}", h.Remove)
	})
	r.Route("/api/v1/reminders", func(r chi.Router) {
		r.Use(authmw.RequireAuth(parser))
		r.Post("/", h.CreateReminder)
		r.Get("/", h.ListReminders)
		r.Get("/{reminderID}", h.GetReminder)
		r.Put("/{reminderID}", h.UpdateReminder)
		r.Delete("/{reminderID}", h.DeleteReminder)
	})
	r.With(internalauth.RequireAuth(internalToken)).Post("/internal/api/v1/reminders/cleanup", h.CleanupReminders)
	return r
}

type reminderRequest struct {
	Title      string              `json:"title"`
	Note       string              `json:"note"`
	EntityType reminder.EntityType `json:"entity_type"`
	EntityID   uuid.UUID           `json:"entity_id"`
	RemindAt   time.Time           `json:"remind_at"`
}

func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	var q reminderRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		httpx.WriteError(w, 400, "invalid_request", "invalid JSON")
		return
	}
	v, e := h.reminders.Create(r.Context(), uid, appnotification.CreateReminderInput{Title: q.Title, Note: q.Note, EntityType: q.EntityType, EntityID: q.EntityID, RemindAt: q.RemindAt})
	if e != nil {
		httpx.WriteError(w, 400, "invalid_reminder", e.Error())
		return
	}
	httpx.WriteJSON(w, 201, v)
}
func parseReminderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "reminderID"))
}
func (h *Handler) GetReminder(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	id, e := parseReminderID(r)
	if e != nil {
		httpx.WriteError(w, 400, "invalid_reminder_id", "invalid reminder id")
		return
	}
	v, e := h.reminders.Get(r.Context(), uid, id)
	if errors.Is(e, reminder.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		httpx.WriteError(w, 500, "get_failed", "could not get reminder")
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (h *Handler) ListReminders(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	p, fields := pagination.ParseParams(r)
	if len(fields) > 0 {
		httpx.WriteError(w, 400, "invalid_pagination", "invalid pagination")
		return
	}
	f := reminder.Filter{Page: p.Page, Limit: p.Limit}
	q := r.URL.Query()
	if x := q.Get("entity_type"); x != "" {
		v := reminder.EntityType(x)
		if !v.Valid() {
			httpx.WriteError(w, 400, "invalid_entity_type", "invalid entity type")
			return
		}
		f.EntityType = &v
	}
	if x := q.Get("entity_id"); x != "" {
		v, e := uuid.Parse(x)
		if e != nil {
			httpx.WriteError(w, 400, "invalid_entity_id", "invalid entity id")
			return
		}
		f.EntityID = &v
	}
	if x := q.Get("status"); x != "" {
		v := reminder.Status(x)
		if v != reminder.StatusScheduled && v != reminder.StatusProcessing && v != reminder.StatusSent && v != reminder.StatusCancelled && v != reminder.StatusFailed {
			httpx.WriteError(w, 400, "invalid_status", "invalid reminder status")
			return
		}
		f.Status = &v
	}
	items, total, e := h.reminders.List(r.Context(), uid, f)
	if e != nil {
		httpx.WriteError(w, 500, "list_failed", "could not list reminders")
		return
	}
	httpx.WriteJSON(w, 200, pagination.NewResponse(items, p, total))
}
func (h *Handler) UpdateReminder(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	id, e := parseReminderID(r)
	if e != nil {
		httpx.WriteError(w, 400, "invalid_reminder_id", "invalid reminder id")
		return
	}
	var q reminderRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		httpx.WriteError(w, 400, "invalid_request", "invalid JSON")
		return
	}
	v, e := h.reminders.Update(r.Context(), uid, id, appnotification.CreateReminderInput{Title: q.Title, Note: q.Note, EntityType: q.EntityType, EntityID: q.EntityID, RemindAt: q.RemindAt})
	if errors.Is(e, reminder.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		httpx.WriteError(w, 400, "invalid_reminder", e.Error())
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (h *Handler) DeleteReminder(w http.ResponseWriter, r *http.Request) {
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, 500, "missing_identity", "authenticated identity missing")
		return
	}
	id, e := parseReminderID(r)
	if e != nil {
		httpx.WriteError(w, 400, "invalid_reminder_id", "invalid reminder id")
		return
	}
	if e = h.reminders.Delete(r.Context(), uid, id); errors.Is(e, reminder.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if e != nil {
		httpx.WriteError(w, 500, "delete_failed", "could not delete reminder")
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) CleanupReminders(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Entities []reminder.EntityRef `json:"entities"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil || len(q.Entities) == 0 {
		httpx.WriteError(w, 400, "invalid_request", "entities are required")
		return
	}
	for _, e := range q.Entities {
		if !e.Type.Valid() || e.ID == uuid.Nil {
			httpx.WriteError(w, 400, "invalid_entity", "invalid entity reference")
			return
		}
	}
	if e := h.reminders.Cleanup(r.Context(), q.Entities); e != nil {
		httpx.WriteError(w, 500, "cleanup_failed", "could not cleanup reminders")
		return
	}
	w.WriteHeader(204)
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
		if errors.Is(err, pushdevice.ErrDestinationOwned) {
			httpx.WriteError(w, http.StatusConflict, "destination_owned", "notification destination is already registered to another user")
			return
		}
		httpx.WriteError(w, 400, "invalid_device", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPushDeviceResponse(d))
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	uid, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, "missing_identity", "authenticated identity missing")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_device_id", "invalid device id")
		return
	}
	d, err := h.service.UpdateDevice(r.Context(), uid, id, req.Destination, req.Platform)
	if err != nil {
		if errors.Is(err, pushdevice.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, pushdevice.ErrDestinationOwned) {
			httpx.WriteError(w, http.StatusConflict, "destination_owned", "notification destination is already registered to another user")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPushDeviceResponse(d))
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
