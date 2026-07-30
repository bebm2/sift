package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/miaoxiaoyong/sift/internal/command"
)

// Command write port (specs/command.md §4/§5). ApplyCommandEvent is the sole
// public command write port; its private transaction primitives own the
// Ledger, Run state and outbox. It never obtains *sql.Tx from a caller and
// never calls Forge, Brain, process checks or signals inside the transaction.

// ErrCommandEffectNotWired means the reason×action pair is canonical but its
// full deterministic effect (Gate re-evaluation operation, new attempt/claim/
// launch, attempt terminalization) is not yet wired in this bootstrap slice.
// The transaction rolls back; no receipt, event or ack is written.
var ErrCommandEffectNotWired = errors.New("storage: command effect not wired")

// ApplyCommandEventCmd is the single write-port input. Parsed is zero for an
// approval_label candidate. Allowlist is the resolved operator logins for the
// Run's forge platform, taken from the immutable config snapshot.
type ApplyCommandEventCmd struct {
	Envelope  command.CommandEventEnvelopeV1
	Parsed    command.ParsedCommand
	Allowlist []string
	NowMS     int64
}

// ApplyCommandEventResult is the persisted outcome. Ignored is true for a
// null/untrusted actor (no event or ack). For a retry request Outcome is
// retry_pending and AckOperationKey is empty (no ack before a final result).
type ApplyCommandEventResult struct {
	Outcome         command.CommandOutcome
	Ignored         bool
	FinalEventID    string
	AckOperationKey string
	NextNonce       string
	InterruptID     string
	RunID           string
}

// ApplyCommandEvent is the only public command write port. It performs
// candidate dedup, allowlist auth, grammar, immutable-target/current-Interrupt/
// nonce/options validation, and the deterministic reason×action effect in one
// transaction, then emits exactly one command event and (for final outcomes)
// one ack operation. There is no second transition, Ledger or outbox path: it
// calls the private transition/recordHumanDecisionTx/insertOperation cores.
func (d *DB) ApplyCommandEvent(ctx context.Context, cmd ApplyCommandEventCmd) (ApplyCommandEventResult, error) {
	if cmd.NowMS <= 0 {
		return ApplyCommandEventResult{}, errors.New("storage: command requires NowMS")
	}
	if err := cmd.Envelope.Validate(); err != nil {
		return ApplyCommandEventResult{}, err
	}
	if !cmd.Envelope.VerifyEventKey() {
		return ApplyCommandEventResult{}, errors.New("storage: command event key mismatch")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplyCommandEventResult{}, err
	}
	defer tx.Rollback()
	res, err := d.applyCommandEventTx(ctx, tx, cmd)
	if err != nil {
		return ApplyCommandEventResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApplyCommandEventResult{}, err
	}
	if res.AckOperationKey != "" {
		d.wakeOutbox()
	}
	return res, nil
}

func (d *DB) applyCommandEventTx(ctx context.Context, tx *sql.Tx, cmd ApplyCommandEventCmd) (ApplyCommandEventResult, error) {
	env := cmd.Envelope

	// 1. Candidate dedup: a duplicate returns the stored outcome and creates no
	// event, Ledger entry, probe or ack.
	if stored, ok, err := lookupCommandReceiptTx(ctx, tx, env); err != nil {
		return ApplyCommandEventResult{}, err
	} else if ok {
		return stored, nil
	}

	// 2. Allowlist auth. A null/untrusted actor persists an ignored receipt and
	// a low-sensitivity security event; it creates no command event or ack.
	authorizer := command.NewAuthorizer(cmd.Allowlist)
	if !authorizer.Trusted(env.Actor) {
		disposition := "ignored_missing_actor"
		if env.Actor != nil && *env.Actor != "" {
			disposition = "ignored_untrusted_actor"
		}
		if err := writeIgnoredCommandReceiptTx(ctx, tx, env, disposition, cmd.NowMS); err != nil {
			return ApplyCommandEventResult{}, err
		}
		audit := map[string]string{"disposition": disposition, "target_kind": string(env.Target.Kind), "target_id": env.Target.ID}
		body, _ := json.Marshal(audit)
		if _, err := tx.ExecContext(ctx, `INSERT INTO events (id,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES (?,?,'command.ignored','forge',1,?,?,?)`,
			newID(), env.ProjectID, string(body), cmd.NowMS, cmd.NowMS); err != nil {
			return ApplyCommandEventResult{}, err
		}
		return ApplyCommandEventResult{Ignored: true}, nil
	}

	// 3. Grammar (forge_comment only; approval_label compiles to approve).
	var parsed command.ParsedCommand
	outcome := command.OutcomeApplied
	if env.Source == command.SourceForgeComment {
		p, perr := command.ParseCommand(env.Comment.Body)
		if perr != nil {
			parsed = command.ParsedCommand{}
			outcome = command.OutcomeRejectedSyntax
		} else {
			parsed = p
		}
	}

	// 4. Immutable target / current Interrupt / nonce / options validation.
	row, bindingTarget, err := resolveCommandInterruptTx(ctx, tx, env)
	if err != nil {
		return ApplyCommandEventResult{}, err
	}
	var view *command.InterruptView
	if row != nil {
		view = interruptView(row)
	}
	var compiled command.CompiledCommandV1
	if outcome == command.OutcomeApplied {
		compileRes := command.Compile(env, parsed, view, bindingTarget)
		outcome = compileRes.Outcome
		compiled = compileRes.Compiled
	}

	// 5. Deterministic effect (only for applied / retry_pending outcomes).
	var nextNonce, finalForEventID, initialEventID string
	skipPersist := false
	if outcome == command.OutcomeApplied && row != nil && row.Reason == string(InterruptStartupStall) && compiled.Action == command.ActionRetry {
		// startup_stall retry is the two-phase request: it creates its own
		// initial retry event + pending outcome + probe here (the event must
		// exist before the probe FK), so persistCommandEventTx is skipped.
		outcome = command.OutcomeRetryPending
		nextNonce, initialEventID, err = d.startupStallRetryRequestTx(ctx, tx, env, compiled, row, cmd.NowMS)
		if err != nil {
			return ApplyCommandEventResult{}, err
		}
		skipPersist = true
	} else if outcome == command.OutcomeApplied {
		nextNonce, finalForEventID, err = d.applyCommandEffectTx(ctx, tx, env, compiled, row, cmd.NowMS)
		if err != nil {
			return ApplyCommandEventResult{}, err
		}
	}

	action := compiled.Action
	if env.Source == command.SourceApprovalLabel {
		action = command.ActionApprove
	}
	if outcome == command.OutcomeRejectedSyntax || outcome == command.OutcomeRejectedTarget {
		action = ""
	}
	runID := ""
	if row != nil {
		runID = row.RunID
	}

	// 6. Command event + outcome relation + receipt + ack operation.
	eventID := initialEventID
	if !skipPersist {
		eventID, err = persistCommandEventTx(ctx, tx, env, outcome, action, runID, compiled.InterruptID, nextNonce, finalForEventID, cmd.NowMS)
		if err != nil {
			return ApplyCommandEventResult{}, err
		}
	}
	ackKey := ""
	if outcome != command.OutcomeRetryPending {
		ackKey = command.AckOperationKey(env.EventKey)
		if err := writeCommandAckOpTx(ctx, tx, env, outcome, action, eventID, compiled.InterruptID, runID, nextNonce, ackKey, cmd.NowMS); err != nil {
			return ApplyCommandEventResult{}, err
		}
	}
	if err := writeAcceptedCommandReceiptTx(ctx, tx, env, "accepted", eventID, cmd.NowMS); err != nil {
		return ApplyCommandEventResult{}, err
	}
	return ApplyCommandEventResult{
		Outcome:         outcome,
		FinalEventID:    eventID,
		AckOperationKey: ackKey,
		NextNonce:       nextNonce,
		InterruptID:     compiled.InterruptID,
		RunID:           runID,
	}, nil
}

// commandInterruptRow is the projection of the current open Interrupt needed
// for compilation and effect dispatch.
type commandInterruptRow struct {
	ID, RunID, Reason, Status, DispatchState, Nonce string
	Version, RunVersion                             int64
	AttemptNo, Generation                           int
	HoldMaxDurationMS                               int64
	Options                                         []string
	ApprovalLabelCutoffPosition                     sql.NullString
}

func resolveCommandInterruptTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1) (*commandInterruptRow, command.CommandTarget, error) {
	var row commandInterruptRow
	var reason, status, state, nonce, optionsJSON string
	var cutoff sql.NullString
	var attemptNo sql.NullInt64
	var targetKind, targetID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT i.id,i.run_id,i.reason,i.status,i.dispatch_state,i.nonce,i.version,r.version,i.attempt_no,i.hold_max_duration_ms,i.options_json,i.approval_label_cutoff_position,t.target_kind,t.target_id
		FROM interrupts i
		JOIN runs r ON r.id=i.run_id
		LEFT JOIN interrupt_command_targets t ON t.interrupt_id=i.id
		WHERE i.status='open' AND t.target_kind=? AND t.target_id=?
		ORDER BY i.created_at_ms DESC, i.id DESC LIMIT 1`, string(env.Target.Kind), env.Target.ID).
		Scan(&row.ID, &row.RunID, &reason, &status, &state, &nonce, &row.Version, &row.RunVersion, &attemptNo, &row.HoldMaxDurationMS, &optionsJSON, &cutoff, &targetKind, &targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, command.CommandTarget{}, nil
	}
	if err != nil {
		return nil, command.CommandTarget{}, err
	}
	row.Reason, row.Status, row.DispatchState, row.Nonce = reason, status, state, nonce
	row.ApprovalLabelCutoffPosition = cutoff
	if attemptNo.Valid {
		row.AttemptNo = int(attemptNo.Int64)
	}
	if row.AttemptNo > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM attempts WHERE run_id=? AND attempt_no=?`, row.RunID, row.AttemptNo).Scan(&row.Generation); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, command.CommandTarget{}, err
		}
	}
	var opts []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
		return nil, command.CommandTarget{}, fmt.Errorf("storage: corrupt interrupt options: %w", err)
	}
	row.Options = make([]string, 0, len(opts))
	for _, o := range opts {
		row.Options = append(row.Options, o.ID)
	}
	binding := command.CommandTarget{}
	if targetKind.Valid && targetID.Valid {
		binding = command.CommandTarget{Kind: command.CommandTargetKind(targetKind.String), ID: targetID.String}
	}
	return &row, binding, nil
}

func interruptView(row *commandInterruptRow) *command.InterruptView {
	var cutoff *string
	if row.ApprovalLabelCutoffPosition.Valid {
		s := row.ApprovalLabelCutoffPosition.String
		cutoff = &s
	}
	return &command.InterruptView{
		ID:                          row.ID,
		RunID:                       row.RunID,
		Version:                     row.Version,
		RunVersion:                  row.RunVersion,
		Reason:                      command.InterruptReason(row.Reason),
		Status:                      command.InterruptStatus(row.Status),
		DispatchState:               command.DispatchState(row.DispatchState),
		Nonce:                       row.Nonce,
		Options:                     row.Options,
		HoldMaxDurationMS:           row.HoldMaxDurationMS,
		ApprovalLabelCutoffPosition: cutoff,
	}
}

// applyCommandEffectTx applies the deterministic reason×action effect for an
// applied (non-startup-retry) command. Returns nextNonce (hold only) and the
// final-for-event id (empty for non-retry).
func (d *DB) applyCommandEffectTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, row *commandInterruptRow, nowMS int64) (string, string, error) {
	if row == nil {
		return "", "", ErrRejectedStale
	}
	switch c.Action {
	case command.ActionReject:
		return d.commandRejectTx(ctx, tx, env, c, row, nowMS)
	case command.ActionHold:
		return d.commandHoldTx(ctx, tx, env, c, nowMS)
	case command.ActionAsk:
		return d.commandAskTx(ctx, tx, env, c, nowMS)
	case command.ActionApprove:
		return d.commandApproveTx(ctx, tx, env, c, row, nowMS)
	case command.ActionRetry:
		// Non-startup retry (failure_review/agent_blocked/merge_conflict)
		// requires Gate re-evaluation / attempt terminalization + new
		// attempt/claim/launch, which is out of this bootstrap slice.
		return "", "", ErrCommandEffectNotWired
	}
	return "", "", ErrCommandEffectNotWired
}

func (d *DB) commandRejectTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, row *commandInterruptRow, nowMS int64) (string, string, error) {
	// reject: close/responded; Run -> failed(human_reject). The bound attempt is
	// marked attempt_resolution=reject (write-once); for startup_stall the
	// isolation is retained until the execution body is proven absent, which is
	// owned by the probe/race path.
	if err := d.transition(ctx, tx, row.RunID, row.RunVersion, DomainCommand{To: RunFailed, Source: SourceOperator, Actor: actorName(env), FailureReason: "human_reject", OccurredAtMS: nowMS}); err != nil {
		return "", "", err
	}
	if row.AttemptNo > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE attempts SET attempt_resolution='reject',resolution_at_ms=?,updated_at_ms=? WHERE run_id=? AND attempt_no=? AND attempt_resolution IS NULL`, nowMS, nowMS, row.RunID, row.AttemptNo); err != nil {
			return "", "", err
		}
	}
	if _, err := recordHumanDecisionTx(ctx, tx, RecordHumanDecisionCmd{
		Action: DecisionReject, CommandEventID: env.EventKey, InterruptID: c.InterruptID,
		SemanticMaterial: c.RejectReason, NowMS: nowMS,
	}); err != nil {
		return "", "", err
	}
	if err := closeInterruptTx(ctx, tx, c.InterruptID, c.ExpectedInterruptVersion, c.Nonce, "responded", nowMS); err != nil {
		return "", "", err
	}
	return "", "", nil
}

func (d *DB) commandHoldTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, nowMS int64) (string, string, error) {
	nextNonce := newToken()
	expires := nowMS + c.HoldDurationMS
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET nonce=?,version=version+1,dispatch_state='held',held_reason='manual',delivery='held',expires_at_ms=?,next_dispatch_at_ms=NULL,nonce_issued_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`,
		nextNonce, expires, nowMS, nowMS, c.InterruptID, c.ExpectedInterruptVersion, c.Nonce)
	if err != nil {
		return "", "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", "", ErrRejectedStale
	}
	if err := excludeStaleBatchMembersTx(ctx, tx, c.InterruptID, nowMS); err != nil {
		return "", "", err
	}
	if _, err := recordHumanDecisionTx(ctx, tx, RecordHumanDecisionCmd{
		Action: DecisionHold, CommandEventID: env.EventKey, InterruptID: c.InterruptID, NowMS: nowMS,
	}); err != nil {
		return "", "", err
	}
	return nextNonce, "", nil
}

func (d *DB) commandAskTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, nowMS int64) (string, string, error) {
	// ask: same-tx task-layer clarification + Ledger semantic material. The Run
	// stays waiting_human; the clarification is recorded, not auto-promoted to
	// project/global Context.
	if _, err := recordHumanDecisionTx(ctx, tx, RecordHumanDecisionCmd{
		Action: DecisionAsk, CommandEventID: env.EventKey, InterruptID: c.InterruptID,
		SemanticMaterial: c.AskText, NowMS: nowMS,
	}); err != nil {
		return "", "", err
	}
	if err := closeInterruptTx(ctx, tx, c.InterruptID, c.ExpectedInterruptVersion, c.Nonce, "responded", nowMS); err != nil {
		return "", "", err
	}
	return "", "", nil
}

func (d *DB) commandApproveTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, row *commandInterruptRow, nowMS int64) (string, string, error) {
	switch InterruptReason(row.Reason) {
	case InterruptDesignApproval:
		// approve: close/responded; waiting_human -> queued. The next pending
		// attempt/claim/launch is enqueued by the launch path on the queued Run.
		if err := d.transition(ctx, tx, row.RunID, row.RunVersion, DomainCommand{To: RunQueued, Source: SourceOperator, Actor: actorName(env), OccurredAtMS: nowMS}); err != nil {
			return "", "", err
		}
	default:
		// guardrail/code_review approve require a Gate re-evaluation operation
		// and an immutable effect fact; out of this bootstrap slice.
		return "", "", ErrCommandEffectNotWired
	}
	if _, err := recordHumanDecisionTx(ctx, tx, RecordHumanDecisionCmd{
		Action: DecisionApprove, CommandEventID: env.EventKey, InterruptID: c.InterruptID, NowMS: nowMS,
	}); err != nil {
		return "", "", err
	}
	if err := closeInterruptTx(ctx, tx, c.InterruptID, c.ExpectedInterruptVersion, c.Nonce, "responded", nowMS); err != nil {
		return "", "", err
	}
	return "", "", nil
}

// startupStallRetryRequestTx is the startup_stall retry request (§5). It does
// not close the Interrupt, create an attempt, release isolation, write a
// resolution or ack. It CAS-rotates the nonce, writes the initial retry event
// + pending outcome relation and a pending probe (referencing that event), and
// records the retry HumanDecision. Returns nextNonce and initialEventID.
func (d *DB) startupStallRetryRequestTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, c command.CompiledCommandV1, row *commandInterruptRow, nowMS int64) (string, string, error) {
	if row == nil || row.AttemptNo < 1 || row.Generation < 1 {
		return "", "", ErrRejectedStale
	}
	nextNonce := newToken()
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET nonce=?,version=version+1,dispatch_state='probe_in_progress',held_reason=NULL,nonce_issued_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND dispatch_state<>'probe_in_progress' AND version=? AND nonce=? AND reason='startup_stall'`,
		nextNonce, nowMS, nowMS, c.InterruptID, c.ExpectedInterruptVersion, c.Nonce)
	if err != nil {
		return "", "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", "", ErrRejectedStale
	}
	// Initial retry event (outcome=retry_pending). It is both the initial
	// command event and the anchor for the pending probe and outcome relation.
	initialEventID, err := persistCommandEventTx(ctx, tx, env, command.OutcomeRetryPending, command.ActionRetry, row.RunID, c.InterruptID, nextNonce, "", nowMS)
	if err != nil {
		return "", "", err
	}
	// Pending probe referencing the initial event. The worker runs after commit.
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempt_probes (id,run_id,attempt_no,interrupt_id,state,expected_run_version,expected_generation,requested_by_event_id,created_at_ms) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
		newID(), row.RunID, row.AttemptNo, c.InterruptID, row.RunVersion, row.Generation, initialEventID, nowMS); err != nil {
		return "", "", err
	}
	if _, err := recordHumanDecisionTx(ctx, tx, RecordHumanDecisionCmd{
		Action: DecisionRetry, CommandEventID: env.EventKey, InterruptID: c.InterruptID, NowMS: nowMS,
	}); err != nil {
		return "", "", err
	}
	return nextNonce, initialEventID, nil
}

// --- persistence helpers ---

func persistCommandEventTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, outcome command.CommandOutcome, action command.CommandAction, runID, interruptID, nextNonce, finalForEventID string, nowMS int64) (string, error) {
	ev := command.NewEvent(env, outcome, action, runID, interruptID, nextNonce, finalForEventID)
	body, err := ev.CanonicalBytes()
	if err != nil {
		return "", err
	}
	initialID := newID()
	idem := command.EventStageKey(env.EventKey, command.StageInitial)
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id,run_id,project_id,type,source,payload_schema_version,payload_json,idempotency_key,occurred_at_ms,recorded_at_ms) VALUES (?,?,?,'command.event','forge',1,?,?,?,?)`,
		initialID, nullable(runID), env.ProjectID, string(body), idem, nowMS, nowMS); err != nil {
		return "", err
	}
	if outcome == command.OutcomeRetryPending {
		if _, err := tx.ExecContext(ctx, `INSERT INTO command_event_outcomes (id,event_key,initial_event_id,final_event_id,state,created_at_ms,finalized_at_ms) VALUES (?,?,?,NULL,'pending',?,NULL)`,
			newID(), env.EventKey, initialID, nowMS); err != nil {
			return "", err
		}
		return initialID, nil
	}
	// final in one transaction (non-retry: initial == final).
	if _, err := tx.ExecContext(ctx, `INSERT INTO command_event_outcomes (id,event_key,initial_event_id,final_event_id,state,created_at_ms,finalized_at_ms) VALUES (?,?,?,?, 'final', ?, ?)`,
		newID(), env.EventKey, initialID, initialID, nowMS, nowMS); err != nil {
		return "", err
	}
	return initialID, nil
}

func writeCommandAckOpTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, outcome command.CommandOutcome, action command.CommandAction, finalEventID, interruptID, runID, nextNonce, ackKey string, nowMS int64) error {
	ev := command.NewEvent(env, outcome, action, runID, interruptID, nextNonce, "")
	ack := command.NewAck(finalEventID, ev)
	body, err := ack.CanonicalBytes()
	if err != nil {
		return err
	}
	op := Operation{Key: ackKey, Kind: OperationCommandAck, Payload: body, InterruptID: interruptID, RunID: runID}
	return insertOperation(ctx, tx, op, runID, interruptID, nowMS)
}

func closeInterruptTx(ctx context.Context, tx *sql.Tx, interruptID string, expectedVersion int64, expectedNonce, closeReason string, nowMS int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET status='closed',close_reason=?,closed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, closeReason, nowMS, nowMS, interruptID, expectedVersion, expectedNonce)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrRejectedStale
	}
	return excludeStaleBatchMembersTx(ctx, tx, interruptID, nowMS)
}

func actorName(env command.CommandEventEnvelopeV1) string {
	if env.Actor == nil {
		return ""
	}
	return *env.Actor
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("storage: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b[:])
}

func lookupCommandReceiptTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1) (ApplyCommandEventResult, bool, error) {
	var disposition, domainEventID string
	var outcomeID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT disposition,domain_event_id,command_outcome_id FROM command_receipts WHERE project_id=? AND event_kind=? AND remote_event_id=?`, env.ProjectID, string(env.Source), env.RemoteEventID).
		Scan(&disposition, &domainEventID, &outcomeID)
	if errors.Is(err, sql.ErrNoRows) {
		return ApplyCommandEventResult{}, false, nil
	}
	if err != nil {
		return ApplyCommandEventResult{}, false, err
	}
	if disposition != "accepted" {
		return ApplyCommandEventResult{Ignored: true}, true, nil
	}
	res := ApplyCommandEventResult{FinalEventID: domainEventID}
	if outcomeID.Valid {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM command_event_outcomes WHERE id=?`, outcomeID.String).Scan(&state); err == nil && state == "pending" {
			res.Outcome = command.OutcomeRetryPending
			return res, true, nil
		}
	}
	res.Outcome = command.OutcomeApplied
	return res, true, nil
}

func writeIgnoredCommandReceiptTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, disposition string, nowMS int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO command_receipts (id,project_id,event_kind,remote_event_id,event_key,target_kind,target_id,actor,raw_digest,disposition,domain_event_id,command_outcome_id,observed_at_ms) VALUES (?,?,?,?,?,?,?,?,?,?,NULL,NULL,?)`,
		newID(), env.ProjectID, string(env.Source), env.RemoteEventID, env.EventKey, string(env.Target.Kind), env.Target.ID, nullablePtr(env.Actor), env.RawDigest, disposition, nowMS)
	return err
}

func writeAcceptedCommandReceiptTx(ctx context.Context, tx *sql.Tx, env command.CommandEventEnvelopeV1, disposition, domainEventID string, nowMS int64) error {
	var outcomeID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id FROM command_event_outcomes WHERE event_key=?`, env.EventKey).Scan(&outcomeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO command_receipts (id,project_id,event_kind,remote_event_id,event_key,target_kind,target_id,actor,raw_digest,disposition,domain_event_id,command_outcome_id,observed_at_ms) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		newID(), env.ProjectID, string(env.Source), env.RemoteEventID, env.EventKey, string(env.Target.Kind), env.Target.ID, nullablePtr(env.Actor), env.RawDigest, disposition, nullable(domainEventID), nullableNullString(outcomeID), nowMS)
	return err
}

func nullablePtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
func nullableNullString(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}
