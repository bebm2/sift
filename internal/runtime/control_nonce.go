package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// controlNonceHash reads the owner-only control.json nonce and returns its
// SHA-256 hex digest. Missing, world-readable, or malformed files yield ""
// so Observe stays incomplete and Terminator fails closed.
func controlNonceHash(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o077 != 0 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var control struct {
		ControlNonce string `json:"control_nonce"`
	}
	if json.Unmarshal(data, &control) != nil || control.ControlNonce == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(control.ControlNonce))
	return hex.EncodeToString(digest[:])
}
