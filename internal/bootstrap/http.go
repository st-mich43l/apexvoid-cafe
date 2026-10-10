package bootstrap

import (
	"io"
	"net/http"
	"path/filepath"
)

func (m *Manager) Handler(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/apexvoid/manifest.json", m.manifest)
	mux.HandleFunc("GET /.well-known/apexvoid/migrations/{name}", m.migration)
	mux.HandleFunc("POST /.well-known/apexvoid/enroll", m.enroll)
	mux.HandleFunc("GET /ready", m.ready)
	mux.Handle("/", next)
	return mux
}

func (m *Manager) manifest(w http.ResponseWriter, r *http.Request) {
	challenge := r.Header.Get("X-ApexVoid-Update-Challenge")
	var body []byte
	var signature string
	var err error
	if challenge != "" {
		body, signature, err = m.SignedUpdateManifest(challenge)
	} else {
		body, signature, err = m.Manifest()
	}
	if err != nil {
		http.Error(w, "manifest unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if challenge != "" {
		w.Header().Set("X-ApexVoid-Update-Signature", signature)
	} else {
		w.Header().Set("X-ApexVoid-Manifest-Signature", signature)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (m *Manager) migration(w http.ResponseWriter, r *http.Request) {
	path, body := m.Migration()
	if filepath.Base(path) != r.PathValue("name") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/sql; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (m *Manager) enroll(w http.ResponseWriter, r *http.Request) {
	challenge := r.Header.Get("X-ApexVoid-Enrollment-Challenge")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid enrollment payload", http.StatusBadRequest)
		return
	}
	ack, err := m.Enroll(r.Context(), body, challenge)
	if err != nil {
		http.Error(w, "enrollment rejected", http.StatusBadRequest)
		return
	}
	w.Header().Set("X-ApexVoid-Enrollment-Ack", ack)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) ready(w http.ResponseWriter, r *http.Request) {
	if !m.Ready(r.Context()) {
		http.Error(w, "application is not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
