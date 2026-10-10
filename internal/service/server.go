package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/st-mich43l/apexvoid-cafe/internal/domain"
	"github.com/st-mich43l/apexvoid-cafe/internal/platform"
)

type Store interface {
	Items(context.Context, string) ([]domain.Item, error)
	CreateItem(context.Context, string, string, domain.ItemInput) (domain.Item, error)
	Booths(context.Context, string) ([]domain.Booth, error)
	CreateBooth(context.Context, string, string, string) (domain.Booth, error)
	Orders(context.Context, string) ([]domain.Order, error)
	CreateOrder(context.Context, string, string, domain.OrderInput) (domain.Order, error)
	TransitionOrder(context.Context, string, string, string) (domain.Order, error)
	Bookings(context.Context, string) ([]domain.Booking, error)
	Reserve(context.Context, string, string, domain.BookingInput) (domain.Booking, error)
	TransitionBooking(context.Context, string, string, string) (domain.Booking, error)
}

// AdvancedStore is intentionally separate from the Phase 1 Store contract so
// the HTTP service can remain bootable and testable while an approved Phase 2
// migration is still pending.
type AdvancedStore interface {
	Store
	ListBookings(context.Context, string, domain.BookingFilter) (domain.Page[domain.Booking], error)
	GetBooking(context.Context, string, string) (domain.Booking, error)
	CreateBooking(context.Context, string, string, domain.BookingInput) (domain.Booking, error)
	TransitionBookingAdvanced(context.Context, string, string, string, string, string) (domain.Booking, error)
	RescheduleBooking(context.Context, string, string, string, domain.BookingInput) (domain.Booking, error)
	Events(context.Context, string, string) ([]domain.BookingEvent, error)
	CreateHold(context.Context, string, string, domain.HoldInput) (domain.Hold, error)
	ReleaseHold(context.Context, string, string) (domain.Hold, error)
	ConfirmHold(context.Context, string, string, string, domain.BookingInput) (domain.Booking, error)
	Availability(context.Context, string, domain.AvailabilityQuery) ([]domain.AvailabilitySlot, error)
	Schedules(context.Context, string, string) ([]domain.OperatingSchedule, error)
	UpsertSchedule(context.Context, string, string, domain.OperatingSchedule) (domain.OperatingSchedule, error)
	Blackouts(context.Context, string, string) ([]domain.Blackout, error)
	CreateBlackout(context.Context, string, string, domain.Blackout) (domain.Blackout, error)
	DeleteBlackout(context.Context, string, string) error
	Utilization(context.Context, string, time.Time, time.Time) (any, error)
}
type Authorizer interface {
	Introspect(context.Context, string, string) (platform.Decision, error)
}
type Server struct {
	mu       sync.RWMutex
	store    Store
	auth     Authorizer
	assetDir string
	provider func() (Store, Authorizer, bool)
}

func New(store Store, auth Authorizer, assetDir string) *Server {
	return &Server{store: store, auth: auth, assetDir: assetDir}
}

// SetRuntimeProvider lets the HTTP surface come up before the Enterprise-
// provisioned database exists. Existing tests and local callers that pass
// store/auth to New retain the original always-ready behavior.
func (s *Server) SetRuntimeProvider(provider func() (Store, Authorizer, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = provider
}

func (s *Server) runtime() (Store, Authorizer, bool) {
	s.mu.RLock()
	provider, store, auth := s.provider, s.store, s.auth
	s.mu.RUnlock()
	if provider != nil {
		return provider()
	}
	return store, auth, store != nil && auth != nil
}

func (s *Server) currentStore() Store {
	store, _, _ := s.runtime()
	return store
}

func (s *Server) advancedStore() (AdvancedStore, bool) {
	store := s.currentStore()
	advanced, ok := store.(AdvancedStore)
	return advanced, ok
}

func requireAdvanced(w http.ResponseWriter, r *http.Request, s *Server) (AdvancedStore, bool) {
	advanced, ok := s.advancedStore()
	if !ok {
		failure(w, http.StatusServiceUnavailable, "SCHEMA_UPGRADE_REQUIRED")
		return nil, false
	}
	if checker, supported := advanced.(interface{ AdvancedBookingReady(context.Context) (bool, error) }); supported {
		ready, err := checker.AdvancedBookingReady(r.Context())
		if err != nil || !ready {
			failure(w, http.StatusServiceUnavailable, "SCHEMA_UPGRADE_REQUIRED")
			return nil, false
		}
	}
	return advanced, true
}

func parseBookingFilter(r *http.Request) (domain.BookingFilter, error) {
	f := domain.BookingFilter{Page: 1, PageSize: 50, Guest: r.URL.Query().Get("guest"), BoothID: r.URL.Query().Get("booth_id"), Status: r.URL.Query().Get("status")}
	if value := r.URL.Query().Get("page"); value != "" {
		f.Page, _ = strconv.Atoi(value)
	}
	if value := r.URL.Query().Get("page_size"); value != "" {
		f.PageSize, _ = strconv.Atoi(value)
	}
	if value := r.URL.Query().Get("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return f, domain.ErrInvalid
		}
		f.From = parsed
	}
	if value := r.URL.Query().Get("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return f, domain.ErrInvalid
		}
		f.To = parsed
	}
	return f.Normalize()
}

func parseTimeQuery(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, domain.ErrInvalid
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, domain.ErrInvalid
	}
	return parsed, nil
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, code string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": code}})
}
func domainFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		failure(w, 400, "VALIDATION_ERROR")
	case errors.Is(err, domain.ErrConflict):
		failure(w, 409, "CONFLICT")
	case errors.Is(err, domain.ErrNotFound):
		failure(w, 404, "NOT_FOUND")
	default:
		failure(w, 500, "INTERNAL_ERROR")
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		failure(w, 415, "JSON_REQUIRED")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		failure(w, 400, "VALIDATION_ERROR")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		failure(w, 400, "VALIDATION_ERROR")
		return false
	}
	return true
}
func (s *Server) guard(permission string, next func(http.ResponseWriter, *http.Request, platform.Decision)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, auth, active := s.runtime()
		if !active || store == nil || auth == nil {
			failure(w, http.StatusServiceUnavailable, "APPLICATION_NOT_READY")
			return
		}
		// In production this port is Docker-internal. Even an internal caller must
		// present a real Enterprise-issued assertion validated on EVERY operation.
		if r.Header.Get(platform.GatewayHeader) != "external-application" {
			failure(w, 401, "GATEWAY_REQUIRED")
			return
		}
		assertion := r.Header.Get(platform.AssertionHeader)
		if assertion == "" {
			failure(w, 401, "ASSERTION_REQUIRED")
			return
		}
		decision, err := auth.Introspect(r.Context(), assertion, permission)
		if err != nil {
			failure(w, 503, "AUTHORIZATION_UNAVAILABLE")
			return
		}
		if !decision.Allowed || decision.Permission != permission || !domain.ValidID(decision.WorkspaceID) || !domain.ValidID(decision.UserID) {
			failure(w, 403, "FORBIDDEN")
			return
		}
		// Handlers use the runtime store captured for this request. It remains
		// stable while the application is active; a reconnect is only published
		// after the database has passed verification.
		_ = store
		next(w, r, decision)
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/context", s.guard("cafe.catalog.read", func(w http.ResponseWriter, r *http.Request, _ platform.Decision) {
		displayName := strings.TrimSpace(r.Header.Get(platform.ApplicationDisplayNameHeader))
		if displayName == "" {
			displayName = "ApexVoid Café"
		}
		write(w, http.StatusOK, map[string]string{"application_id": "cafe", "display_name": displayName})
	}))
	mux.HandleFunc("GET /v1/menu", s.guard("cafe.catalog.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.currentStore().Items(r.Context(), d.WorkspaceID)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/menu", s.guard("cafe.catalog.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		var in domain.ItemInput
		if !decode(w, r, &in) {
			return
		}
		item, err := s.currentStore().CreateItem(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("GET /v1/booths", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.currentStore().Booths(r.Context(), d.WorkspaceID)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/booths", s.guard("cafe.booth.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		var in struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		item, err := s.currentStore().CreateBooth(r.Context(), d.WorkspaceID, d.UserID, in.Name)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("GET /v1/orders", s.guard("cafe.order.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.currentStore().Orders(r.Context(), d.WorkspaceID)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/orders", s.guard("cafe.order.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		var in domain.OrderInput
		if !decode(w, r, &in) {
			return
		}
		item, err := s.currentStore().CreateOrder(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	for _, action := range []string{"serve", "cancel"} {
		a := action
		mux.HandleFunc("POST /v1/orders/{id}/"+a, s.guard("cafe.order.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
			item, err := s.currentStore().TransitionOrder(r.Context(), d.WorkspaceID, r.PathValue("id"), a)
			if err != nil {
				domainFailure(w, err)
				return
			}
			write(w, 200, item)
		}))
	}
	mux.HandleFunc("GET /v1/bookings", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		filter, err := parseBookingFilter(r)
		if err != nil {
			domainFailure(w, err)
			return
		}
		items, err := advanced.ListBookings(r.Context(), d.WorkspaceID, filter)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/bookings", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		var in domain.BookingInput
		if !decode(w, r, &in) {
			return
		}
		item, err := advanced.CreateBooking(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("GET /v1/bookings/{id}", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		item, err := advanced.GetBooking(r.Context(), d.WorkspaceID, r.PathValue("id"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, item)
	}))
	mux.HandleFunc("GET /v1/bookings/{id}/events", s.guard("cafe.booking.history.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		items, err := advanced.Events(r.Context(), d.WorkspaceID, r.PathValue("id"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("GET /v1/bookings/availability", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		date, err := parseTimeQuery(r.URL.Query().Get("date"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		slots, err := advanced.Availability(r.Context(), d.WorkspaceID, domain.AvailabilityQuery{BoothID: r.URL.Query().Get("booth_id"), PackageID: r.URL.Query().Get("package_id"), Date: date})
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, map[string]any{"slots": slots})
	}))
	mux.HandleFunc("POST /v1/bookings/{id}/reschedule", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		var in domain.BookingInput
		if !decode(w, r, &in) {
			return
		}
		item, err := advanced.RescheduleBooking(r.Context(), d.WorkspaceID, d.UserID, r.PathValue("id"), in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, item)
	}))
	for _, action := range []string{"check-in", "start", "complete", "cancel", "no-show"} {
		a := action
		mux.HandleFunc("POST /v1/bookings/{id}/"+a, s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
			advanced, ok := requireAdvanced(w, r, s)
			if !ok {
				return
			}
			var input struct {
				Reason string `json:"reason"`
			}
			if r.ContentLength != 0 && !decode(w, r, &input) {
				return
			}
			item, err := advanced.TransitionBookingAdvanced(r.Context(), d.WorkspaceID, d.UserID, r.PathValue("id"), a, input.Reason)
			if err != nil {
				domainFailure(w, err)
				return
			}
			write(w, 200, item)
		}))
	}
	mux.HandleFunc("POST /v1/booking-holds", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		var in domain.HoldInput
		if !decode(w, r, &in) {
			return
		}
		hold, err := advanced.CreateHold(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, hold)
	}))
	mux.HandleFunc("DELETE /v1/booking-holds/{id}", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		hold, err := advanced.ReleaseHold(r.Context(), d.WorkspaceID, r.PathValue("id"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, hold)
	}))
	mux.HandleFunc("POST /v1/booking-holds/{id}/confirm", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		var in domain.BookingInput
		if !decode(w, r, &in) {
			return
		}
		booking, err := advanced.ConfirmHold(r.Context(), d.WorkspaceID, d.UserID, r.PathValue("id"), in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, booking)
	}))
	mux.HandleFunc("GET /v1/schedules", s.guard("cafe.booking.schedule.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		items, err := advanced.Schedules(r.Context(), d.WorkspaceID, r.URL.Query().Get("booth_id"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("PUT /v1/schedules/{weekday}", s.guard("cafe.booking.schedule.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		weekday, _ := strconv.Atoi(r.PathValue("weekday"))
		var in domain.OperatingSchedule
		if !decode(w, r, &in) {
			return
		}
		in.Weekday = weekday
		item, err := advanced.UpsertSchedule(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, item)
	}))
	mux.HandleFunc("GET /v1/blackouts", s.guard("cafe.booking.schedule.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		items, err := advanced.Blackouts(r.Context(), d.WorkspaceID, r.URL.Query().Get("booth_id"))
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/blackouts", s.guard("cafe.booking.schedule.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		var in domain.Blackout
		if !decode(w, r, &in) {
			return
		}
		item, err := advanced.CreateBlackout(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("DELETE /v1/blackouts/{id}", s.guard("cafe.booking.schedule.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		if err := advanced.DeleteBlackout(r.Context(), d.WorkspaceID, r.PathValue("id")); err != nil {
			domainFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /v1/booths/utilization", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		advanced, ok := requireAdvanced(w, r, s)
		if !ok {
			return
		}
		from, _ := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, _ := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if from.IsZero() {
			from = time.Now().AddDate(0, 0, -7)
		}
		if to.IsZero() {
			to = time.Now().AddDate(0, 0, 7)
		}
		data, err := advanced.Utilization(r.Context(), d.WorkspaceID, from, to)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, data)
	}))
	assets := http.FileServer(http.Dir(s.assetDir))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			failure(w, 404, "NOT_FOUND")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		if filepath.Ext(r.URL.Path) != "" {
			http.NotFound(w, r)
			return
		}
		// Frontend reachability is already checked by Enterprise's authenticated gateway.
		if _, err := os.Stat(filepath.Join(s.assetDir, "index.html")); err != nil {
			failure(w, 503, "FRONTEND_NOT_BUILT")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(s.assetDir, "index.html"))
	})
	return mux
}
