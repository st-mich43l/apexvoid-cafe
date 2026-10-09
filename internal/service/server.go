package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
type Authorizer interface {
	Introspect(context.Context, string, string) (platform.Decision, error)
}
type Server struct {
	store    Store
	auth     Authorizer
	assetDir string
}

func New(store Store, auth Authorizer, assetDir string) *Server {
	return &Server{store: store, auth: auth, assetDir: assetDir}
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
		decision, err := s.auth.Introspect(r.Context(), assertion, permission)
		if err != nil {
			failure(w, 503, "AUTHORIZATION_UNAVAILABLE")
			return
		}
		if !decision.Allowed || decision.Permission != permission || !domain.ValidID(decision.WorkspaceID) || !domain.ValidID(decision.UserID) {
			failure(w, 403, "FORBIDDEN")
			return
		}
		next(w, r, decision)
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/menu", s.guard("cafe.catalog.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.store.Items(r.Context(), d.WorkspaceID)
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
		item, err := s.store.CreateItem(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("GET /v1/booths", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.store.Booths(r.Context(), d.WorkspaceID)
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
		item, err := s.store.CreateBooth(r.Context(), d.WorkspaceID, d.UserID, in.Name)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	mux.HandleFunc("GET /v1/orders", s.guard("cafe.order.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.store.Orders(r.Context(), d.WorkspaceID)
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
		item, err := s.store.CreateOrder(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	for _, action := range []string{"serve", "cancel"} {
		a := action
		mux.HandleFunc("POST /v1/orders/{id}/"+a, s.guard("cafe.order.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
			item, err := s.store.TransitionOrder(r.Context(), d.WorkspaceID, r.PathValue("id"), a)
			if err != nil {
				domainFailure(w, err)
				return
			}
			write(w, 200, item)
		}))
	}
	mux.HandleFunc("GET /v1/bookings", s.guard("cafe.booking.read", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		items, err := s.store.Bookings(r.Context(), d.WorkspaceID)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 200, items)
	}))
	mux.HandleFunc("POST /v1/bookings", s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
		var in domain.BookingInput
		if !decode(w, r, &in) {
			return
		}
		item, err := s.store.Reserve(r.Context(), d.WorkspaceID, d.UserID, in)
		if err != nil {
			domainFailure(w, err)
			return
		}
		write(w, 201, item)
	}))
	for _, action := range []string{"check-in", "complete", "cancel"} {
		a := action
		mux.HandleFunc("POST /v1/bookings/{id}/"+a, s.guard("cafe.booking.manage", func(w http.ResponseWriter, r *http.Request, d platform.Decision) {
			item, err := s.store.TransitionBooking(r.Context(), d.WorkspaceID, r.PathValue("id"), a)
			if err != nil {
				domainFailure(w, err)
				return
			}
			write(w, 200, item)
		}))
	}
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
