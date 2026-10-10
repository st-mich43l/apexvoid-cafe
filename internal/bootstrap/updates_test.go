package bootstrap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManifestUpdateRequiresEnrolledCredentialAndFreshChallenge(t *testing.T) {
	m := &Manager{
		state: persistedState{Phase: "ACTIVE", SetupCode: strings.Repeat("c", 40), ServiceCredential: strings.Repeat("s", 64)},
		manifestJSON: []byte("{\"manifest_version\":\"v1\"}"),
	}
	challenge := strings.Repeat("n", 40)
	request := httptest.NewRequest(http.MethodGet, "/.well-known/apexvoid/manifest.json", nil)
	request.Header.Set("X-ApexVoid-Update-Challenge", challenge)
	response := httptest.NewRecorder()
	m.manifest(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("manifest status = %d", response.Code)
	}
	if response.Header().Get("X-ApexVoid-Manifest-Signature") != "" {
		t.Fatal("setup signature exposed in update response")
	}
	credentialHash := sha256.Sum256([]byte(m.state.ServiceCredential))
	mac := hmac.New(sha256.New, credentialHash[:])
	_, _ = mac.Write([]byte("apexvoid-update-manifest-v1\n" + challenge + "\n"))
	_, _ = mac.Write(m.manifestJSON)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if response.Header().Get("X-ApexVoid-Update-Signature") != expected {
		t.Fatal("update manifest is not signed against the current service credential and challenge")
	}
	if response.Body.String() != string(m.manifestJSON) {
		t.Fatal("response changed the exact signed manifest bytes")
	}
	m.state.Phase = "PENDING_REGISTRATION"
	response = httptest.NewRecorder()
	m.manifest(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("update discovery allowed before permanent credential enrollment")
	}
}
