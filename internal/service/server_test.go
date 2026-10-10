package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/st-mich43l/apexvoid-photobooth/internal/domain"
	"github.com/st-mich43l/apexvoid-photobooth/internal/platform"
)

const userID = "123e4567-e89b-42d3-a456-426614174000"
const workspaceID = "123e4567-e89b-42d3-a456-426614174001"

type fakeAuth struct {
	allowed bool
	calls   []string
}

func (a *fakeAuth) Introspect(_ context.Context, assertion, permission string) (platform.Decision, error) {
	a.calls = append(a.calls, permission)
	return platform.Decision{Permission: permission, UserID: userID, WorkspaceID: workspaceID, Allowed: a.allowed}, nil
}

type fakeStore struct {
	workspace string
	created   bool
}

func (s *fakeStore) Items(_ context.Context, w string) ([]domain.Item, error) {
	s.workspace = w
	return []domain.Item{}, nil
}
func (s *fakeStore) CreateItem(_ context.Context, w, u string, i domain.ItemInput) (domain.Item, error) {
	s.created = true
	s.workspace = w
	return domain.Item{Name: i.Name}, nil
}
func (s *fakeStore) Booths(_ context.Context, w string) ([]domain.Booth, error) {
	return []domain.Booth{}, nil
}
func (s *fakeStore) CreateBooth(_ context.Context, w, u, n string) (domain.Booth, error) {
	return domain.Booth{}, nil
}
func (s *fakeStore) Orders(_ context.Context, w string) ([]domain.Order, error) {
	return []domain.Order{}, nil
}
func (s *fakeStore) CreateOrder(_ context.Context, w, u string, i domain.OrderInput) (domain.Order, error) {
	return domain.Order{}, nil
}
func (s *fakeStore) TransitionOrder(_ context.Context, w, id, action string) (domain.Order, error) {
	return domain.Order{}, nil
}
func (s *fakeStore) Bookings(_ context.Context, w string) ([]domain.Booking, error) {
	return []domain.Booking{}, nil
}
func (s *fakeStore) Reserve(_ context.Context, w, u string, i domain.BookingInput) (domain.Booking, error) {
	return domain.Booking{}, nil
}
func (s *fakeStore) TransitionBooking(_ context.Context, w, id, action string) (domain.Booking, error) {
	return domain.Booking{}, nil
}
func TestNoBypassOfGatewayAndPermission(t *testing.T) {
	auth := &fakeAuth{allowed: true}
	store := &fakeStore{}
	app := New(store, auth, t.TempDir()).Handler()
	call := func(method, path, assertion string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if assertion != "" {
			req.Header.Set(platform.GatewayHeader, "external-application")
			req.Header.Set(platform.AssertionHeader, assertion)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		return w
	}
	if got := call("GET", "/v1/menu", "").Code; got != 401 {
		t.Fatalf("untrusted call %d", got)
	}
	if len(auth.calls) != 0 {
		t.Fatal("untrusted caller reached introspection")
	}
	if got := call("GET", "/v1/menu", "real").Code; got != 200 {
		t.Fatalf("allowed call %d", got)
	}
	if len(auth.calls) != 1 || auth.calls[0] != "photobooth.catalog.read" || store.workspace != workspaceID {
		t.Fatal("operation or tenant not bound to introspection")
	}
	auth.allowed = false
	if got := call("GET", "/v1/menu", "real").Code; got != 403 {
		t.Fatalf("denied call %d", got)
	}
}

func TestContextUsesEnterpriseManagedDisplayName(t *testing.T) {
	auth := &fakeAuth{allowed: true}
	app := New(&fakeStore{}, auth, t.TempDir()).Handler()
	req := httptest.NewRequest("GET", "/v1/context", nil)
	req.Header.Set(platform.GatewayHeader, "external-application")
	req.Header.Set(platform.AssertionHeader, "real")
	req.Header.Set(platform.ApplicationDisplayNameHeader, "HUI moment")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("context request returned %d", res.Code)
	}
	var body struct {
		ApplicationID string `json:"application_id"`
		DisplayName   string `json:"display_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ApplicationID != "photobooth" || body.DisplayName != "HUI moment" {
		t.Fatalf("unexpected context: %+v", body)
	}
}

func TestUnknownFieldsFailClosed(t *testing.T) {
	auth := &fakeAuth{allowed: true}
	store := &fakeStore{}
	app := New(store, auth, t.TempDir()).Handler()
	req := httptest.NewRequest("POST", "/v1/menu", bytes.NewBufferString(`{"name":"Coffee","sku":"C1","kind":"drink","price_vnd":49000,"workspace_id":"attacker"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(platform.AssertionHeader, "legitimate")
	req.Header.Set(platform.GatewayHeader, "external-application")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	if res.Code != 400 || store.created {
		t.Fatalf("workspace spoof not rejected: %d", res.Code)
	}
}

func TestBusinessRoutesStayUnavailableBeforeEnrollment(t *testing.T) {
	app := New(nil, nil, t.TempDir())
	app.SetRuntimeProvider(func() (Store, Authorizer, bool) { return nil, nil, false })
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/v1/menu", nil))
	if res.Code != 503 {
		t.Fatalf("pre-enrollment business route returned %d", res.Code)
	}
}
