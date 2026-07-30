package command

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// CommandAction is the closed action set from PRD §7.1.
type CommandAction string

const (
	ActionApprove CommandAction = "approve"
	ActionReject  CommandAction = "reject"
	ActionRetry   CommandAction = "retry"
	ActionHold    CommandAction = "hold"
	ActionAsk     CommandAction = "ask"
)

const (
	hexLen         = 32
	sp        byte = 0x20
	maxReason      = 16384
)

// ErrSyntax is returned by ParseCommand for any grammar violation (§3.1). A
// syntax failure is still a trusted candidate: the transaction writes an
// accepted receipt, a closed rejection event and its ack.
var ErrSyntax = errors.New("command: syntax")

// ParsedCommand is the output of a successful grammar parse. Approval-label
// candidates never enter the grammar: they compile directly to ActionApprove.
type ParsedCommand struct {
	Action         CommandAction
	RunID          string
	Nonce          string
	HoldDurationMS int64
	RejectReason   string
	AskText        string
}

// ParseCommand parses a forge_comment body against the §3.1 byte grammar. The
// entire body must be exactly one command: one ASCII space (0x20) separates
// fields and EOF immediately follows the final byte. Any deviation is a syntax
// error. Natural-language inputs (reject reason, ask text) are returned
// unmodified; only structural validity is checked here.
func ParseCommand(body string) (ParsedCommand, error) {
	if body == "" {
		return ParsedCommand{}, ErrSyntax
	}
	// The leading literal is "/sift " (with one trailing space).
	const prefix = "/sift"
	if len(body) < len(prefix)+1 || body[:len(prefix)] != prefix || body[len(prefix)] != sp {
		return ParsedCommand{}, ErrSyntax
	}
	rest := body[len(prefix)+1:]
	// Action token runs to the next space or EOF.
	actionWord, after, ok := nextToken(rest)
	if !ok || actionWord == "" {
		return ParsedCommand{}, ErrSyntax
	}
	action := CommandAction(actionWord)
	switch action {
	case ActionApprove:
		return parseApprove(after)
	case ActionReject:
		return parseReject(after)
	case ActionRetry:
		return parseRetry(after)
	case ActionHold:
		return parseHold(after)
	case ActionAsk:
		return parseAsk(after)
	default:
		return ParsedCommand{}, ErrSyntax
	}
}

// nextToken splits at the first 0x20. It returns the token, the remainder
// (without the separating space) and ok=false if a leading/trailing/double
// space invariant is violated. EOF after the token is allowed.
func nextToken(s string) (token, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if s[0] == sp {
		return "", "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == sp {
			// Must be followed by at least one non-space byte.
			if i+1 >= len(s) || s[i+1] == sp {
				return "", "", false
			}
			return s[:i], s[i+1:], true
		}
	}
	return s, "", true
}

func parseApprove(rest string) (ParsedCommand, error) {
	runID, after, ok := nextToken(rest)
	if !ok || !isFixedHex(runID) {
		return ParsedCommand{}, ErrSyntax
	}
	nonce, after2, ok := nextToken(after)
	if !ok || after2 != "" || !isFixedHex(nonce) {
		return ParsedCommand{}, ErrSyntax
	}
	return ParsedCommand{Action: ActionApprove, RunID: runID, Nonce: nonce}, nil
}

func parseRetry(rest string) (ParsedCommand, error) {
	runID, after, ok := nextToken(rest)
	if !ok || !isFixedHex(runID) {
		return ParsedCommand{}, ErrSyntax
	}
	nonce, after2, ok := nextToken(after)
	if !ok || after2 != "" || !isFixedHex(nonce) {
		return ParsedCommand{}, ErrSyntax
	}
	return ParsedCommand{Action: ActionRetry, RunID: runID, Nonce: nonce}, nil
}

func parseReject(rest string) (ParsedCommand, error) {
	runID, after, ok := nextToken(rest)
	if !ok || !isFixedHex(runID) {
		return ParsedCommand{}, ErrSyntax
	}
	nonce, after2, ok := nextToken(after)
	if !ok || !isFixedHex(nonce) {
		return ParsedCommand{}, ErrSyntax
	}
	// Optional reason: everything after the separating space.
	if after2 == "" {
		return ParsedCommand{Action: ActionReject, RunID: runID, Nonce: nonce}, nil
	}
	if !validReason(after2) {
		return ParsedCommand{}, ErrSyntax
	}
	return ParsedCommand{Action: ActionReject, RunID: runID, Nonce: nonce, RejectReason: after2}, nil
}

func parseHold(rest string) (ParsedCommand, error) {
	runID, after, ok := nextToken(rest)
	if !ok || !isFixedHex(runID) {
		return ParsedCommand{}, ErrSyntax
	}
	nonce, after2, ok := nextToken(after)
	if !ok || !isFixedHex(nonce) {
		return ParsedCommand{}, ErrSyntax
	}
	dur, after3, ok := nextToken(after2)
	if !ok || after3 != "" {
		return ParsedCommand{}, ErrSyntax
	}
	ms, err := parseDurationMS(dur)
	if err != nil {
		return ParsedCommand{}, err
	}
	return ParsedCommand{Action: ActionHold, RunID: runID, Nonce: nonce, HoldDurationMS: ms}, nil
}

func parseAsk(rest string) (ParsedCommand, error) {
	runID, after, ok := nextToken(rest)
	if !ok || !isFixedHex(runID) {
		return ParsedCommand{}, ErrSyntax
	}
	nonce, after2, ok := nextToken(after)
	if !ok || !isFixedHex(nonce) {
		return ParsedCommand{}, ErrSyntax
	}
	// text is required: 1–16384 UTF-8 bytes, LF/CRLF allowed, bare CR rejected.
	if after2 == "" || !validAskText(after2) {
		return ParsedCommand{}, ErrSyntax
	}
	return ParsedCommand{Action: ActionAsk, RunID: runID, Nonce: nonce, AskText: after2}, nil
}

func isFixedHex(s string) bool {
	if len(s) != hexLen {
		return false
	}
	return isLowerHex(s)
}

// validReason enforces reason: 1–16384 UTF-8 bytes, no NUL, no CR, no LF.
func validReason(s string) bool {
	if len(s) == 0 || len(s) > maxReason {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 0x00, '\n', '\r':
			return false
		}
	}
	return true
}

// validAskText enforces text: 1–16384 UTF-8 bytes, no NUL, no bare CR. LF and
// CRLF are accepted exactly as supplied.
func validAskText(s string) bool {
	if len(s) == 0 || len(s) > maxReason {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == 0x00 {
			return false
		}
		if s[i] == '\r' {
			// Bare CR (not followed by LF) is rejected.
			if i+1 >= len(s) || s[i+1] != '\n' {
				return false
			}
		}
	}
	return true
}

// parseDurationMS parses the positive, unsigned subset of Go time.ParseDuration
// and returns integral milliseconds. It rejects overflow, <=0 results and any
// sub-millisecond remainder (§3.1).
func parseDurationMS(s string) (int64, error) {
	if s == "" {
		return 0, ErrSyntax
	}
	// Reject a leading sign explicitly: the grammar is unsigned. ParseDuration
	// would accept "-5s" (then we reject <=0) and "+5s" (error); reject the sign
	// up front so the byte grammar, not float arithmetic, owns the boundary.
	if s[0] == '+' || s[0] == '-' {
		return 0, ErrSyntax
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, ErrSyntax
	}
	if d <= 0 {
		return 0, ErrSyntax
	}
	if d%time.Millisecond != 0 {
		return 0, ErrSyntax
	}
	ms := int64(d / time.Millisecond)
	if ms <= 0 {
		return 0, fmt.Errorf("%w: duration overflow", ErrSyntax)
	}
	return ms, nil
}
