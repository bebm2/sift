package forge

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
)

var markerRE = regexp.MustCompile(`<!-- sift-op:v1:([^:>]+):([0-9a-f]{64}) -->`)

// OperationMarker is deterministic and contains no user-controlled markup.
func OperationMarker(operationKey, payloadDigest string) string {
	enc := base64.RawURLEncoding.EncodeToString([]byte(operationKey))
	return "<!-- sift-op:v1:" + enc + ":" + payloadDigest + " -->"
}
func PayloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
func RenderOperationBody(markdown, operationKey, payloadDigest string) string {
	markdown = markerRE.ReplaceAllString(markdown, "")
	return strings.TrimRight(markdown, "\n") + "\n\n" + OperationMarker(operationKey, payloadDigest)
}
func FindOperationMarker(body, operationKey, payloadDigest string) bool {
	return strings.Contains(body, OperationMarker(operationKey, payloadDigest))
}
