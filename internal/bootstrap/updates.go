package bootstrap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// SignedUpdateManifest returns the installed application's exact manifest
// bytes authenticated by the current Enterprise service credential.
// No permanent credential is transmitted in the manifest request.
func (m *Manager) SignedUpdateManifest(challenge string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state.Phase != "ACTIVE" || len(m.state.ServiceCredential) < 32 || len(challenge) < 32 {
		return nil, "", errors.New("signed update discovery is unavailable")
	}
	body := append([]byte(nil), m.manifestJSON...)
	key := sha256.Sum256([]byte(m.state.ServiceCredential))
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte("apexvoid-update-manifest-v1\n" + challenge + "\n"))
	_, _ = mac.Write(body)
	return body, "sha256=" + hex.EncodeToString(mac.Sum(nil)), nil
}
