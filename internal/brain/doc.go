// Package brain implements the unified Brain call shell of specs/brain.md:
// pre-attempt budget gate → provider subprocess → open-envelope decode →
// closed touchpoint decode → same-prompt retry once → deterministic
// touchpoint fallback. Every logical call and physical attempt is persisted
// through the storage brain ports, tokens are post-charged at the single
// charging point, and prompts/output schemas are versioned assets embedded
// from git (prompts/<touchpoint>/v<N>.md + v<N>.schema.json).
//
// The LLM only ever produces recommendations; dispositions, assignments,
// budgets and state transitions remain deterministic code (brain.md §1).
package brain
