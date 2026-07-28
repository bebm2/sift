// Package config implements the global configuration contract of
// specs/config.md: the closed schema for ~/.sift/config.yaml, SIFT_HOME path
// resolution, zero-config defaults, the sensitive-config fingerprint, the
// warn-only drift detector, the two-level startup probe framework and the
// scheduling hard guards.
//
// The package is the second consumer of the single decode gateway
// (internal/decode, DESIGN §5.2). The on-disk file is YAML; a strict YAML→JSON
// bridge converts it once, then [decode.Closed] enforces the closed contract
// (unknown fields, required fields, type and enum rules). Business code never
// reads YAML or JSON directly: the only entry point is [Load].
//
// V0 does not hot-reload global config (config.md §1.3). [Load] reads the file
// once at daemon startup, normalizes it, computes a canonical-JSON fingerprint
// and hands back an immutable in-memory snapshot. On-disk changes are observed
// only by the [DriftChecker], which appends one security event and a doctor
// warning but never applies the new content.
package config

// Version is the single supported global-config schema version (config.md §3).
const Version = 1
