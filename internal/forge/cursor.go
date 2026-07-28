package forge

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Cursor is deliberately opaque outside this package. The timestamp narrows
// the remote query; ID records a stable tie-breaker. Calls query the timestamp
// inclusively, so records at the boundary can be replayed rather than skipped.
type Cursor string

type cursorState struct {
	Time string `json:"time"`
	ID   string `json:"id"`
}

type cursorTracker struct {
	previous Cursor
	latest   cursorState
	seen     map[string]struct{}
}

func newCursorTracker(c Cursor) (*cursorTracker, error) {
	t := &cursorTracker{previous: c, seen: make(map[string]struct{})}
	if c == "" {
		return t, nil
	}
	encoded, err := base64.RawURLEncoding.DecodeString(string(c))
	if err != nil {
		return nil, &ClassifiedError{Class: ErrContractViolation, Summary: "invalid forge cursor"}
	}
	if err := json.Unmarshal(encoded, &t.latest); err != nil || t.latest.Time == "" || t.latest.ID == "" {
		return nil, &ClassifiedError{Class: ErrContractViolation, Summary: "invalid forge cursor"}
	}
	if _, err := time.Parse(time.RFC3339Nano, t.latest.Time); err != nil {
		return nil, &ClassifiedError{Class: ErrContractViolation, Summary: "invalid forge cursor"}
	}
	return t, nil
}

func (t *cursorTracker) queryTime() string {
	if t.latest.Time == "" {
		return ""
	}
	at, _ := time.Parse(time.RFC3339Nano, t.latest.Time)
	// GitLab's updated_after is exclusive. Query an earlier boundary on both
	// platforms so a page split at one timestamp is replayed, never skipped.
	return at.Add(-time.Second).Format(time.RFC3339Nano)
}

func (t *cursorTracker) add(id string, at time.Time) (bool, error) {
	if id == "" || id == "0" || at.IsZero() {
		return false, &ClassifiedError{Class: ErrContractViolation, Summary: "incremental record missing id or timestamp"}
	}
	if _, ok := t.seen[id]; ok {
		return false, nil
	}
	t.seen[id] = struct{}{}
	candidate := cursorState{Time: at.UTC().Format(time.RFC3339Nano), ID: id}
	if t.latest.Time == "" || candidate.Time > t.latest.Time || (candidate.Time == t.latest.Time && candidate.ID > t.latest.ID) {
		t.latest = candidate
	}
	return true, nil
}

func (t *cursorTracker) next() Cursor {
	if t.latest.Time == "" {
		return t.previous
	}
	b, _ := json.Marshal(t.latest)
	return Cursor(base64.RawURLEncoding.EncodeToString(b))
}
