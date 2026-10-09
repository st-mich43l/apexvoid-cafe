package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIntrospectionUsesCredentialAndOperationPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/integrations/v1/session/introspect" || r.Header.Get("X-ApexVoid-Application-ID") != "cafe" || r.Header.Get("X-ApexVoid-Service-Credential") != "secret" {
			t.Errorf("unexpected headers or route")
			w.WriteHeader(401)
			return
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["identity_assertion"] != "assertion" || req["permission"] != "cafe.order.read" {
			t.Errorf("unexpected authorization request %#v %v", req, err)
		}
		json.NewEncoder(w).Encode(Decision{UserID: "user", WorkspaceID: "workspace", Permission: "cafe.order.read", Allowed: true})
	}))
	defer server.Close()
	c, err := New(server.URL, "cafe", "secret")
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Introspect(context.Background(), "assertion", "cafe.order.read")
	if err != nil || !d.Allowed {
		t.Fatalf("unexpected decision %#v %v", d, err)
	}
}
func TestRefusesRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	c, _ := New(server.URL, "cafe", "topsecret")
	_, err := c.Introspect(context.Background(), "x", "cafe.order.read")
	if err == nil || called || strings.Contains(err.Error(), "topsecret") {
		t.Fatal("credential escaped on redirect")
	}
}
