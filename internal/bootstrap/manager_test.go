package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/st-mich43l/apexvoid-CRM/integration"
)

func TestManifestIsSignedFromPublishedMigrationBytes(t *testing.T) {
	dir := t.TempDir()
	migration := []byte("CREATE TABLE cafe.example (id integer PRIMARY KEY);\n")
	path := filepath.Join(dir, "001_cafe.sql")
	if err := os.WriteFile(path, migration, 0600); err != nil {
		t.Fatal(err)
	}
	m, first, err := New(Config{StateDir: filepath.Join(dir, "state"), MigrationPath: path, PlatformURL: "http://backend:6868"})
	if err != nil || !first {
		t.Fatalf("New() = first=%v err=%v", first, err)
	}
	body, signature, err := m.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := integration.SignManifest(m.SetupCodeForOperator(), body)
	if err != nil || expected != signature {
		t.Fatalf("manifest signature mismatch: %q %q %v", expected, signature, err)
	}
	var decoded struct {
		Application struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"application"`
		Database struct {
			Name   string `json:"name"`
			Schema string `json:"schema"`
			Role   string `json:"role"`
		} `json:"database"`
		Migrations []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Application.ID != "cafe" || decoded.Application.Version != "0.1.0" || decoded.Database.Name != "apexvoid_cafe" || decoded.Database.Schema != "cafe" || decoded.Database.Role != "apexvoid_cafe" || len(decoded.Migrations) != 1 {
		t.Fatalf("unexpected manifest: %s", body)
	}
	if decoded.Migrations[0].Path != "/.well-known/apexvoid/migrations/001_cafe.sql" || decoded.Migrations[0].SHA256 == "" {
		t.Fatalf("migration is not pinned: %s", body)
	}

	server := httptest.NewServer(m.Handler(http.NotFoundHandler()))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/.well-known/apexvoid/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("X-ApexVoid-Manifest-Signature") != signature {
		t.Fatalf("unexpected manifest response: %d %s", response.StatusCode, response.Header.Get("X-ApexVoid-Manifest-Signature"))
	}
}
