package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// IntakeItemInput is the durable, platform-neutral result of one forge poll.
type IntakeItemInput struct {
	IssueID, IssueURL, IssueDigest string
	ForgeKind, Host, ProjectKey    string
	EventID, EventKind, TargetKind string
	Actor                          string
	ObservedAtMS                   int64
	RawDigest                      string
}

type PersistIntakeBatchCmd struct {
	ProjectID string
	Stream    string
	Cursor    string
	NowMS     int64
	Items     []IntakeItemInput
}

// PersistIntakeBatch writes receipts and pending intake projections before
// advancing the cursor. Replaying a forge page is consequently harmless, and
// a failure leaves the cursor at its previous value.
func (d *DB) PersistIntakeBatch(ctx context.Context, cmd PersistIntakeBatchCmd) error {
	if cmd.ProjectID == "" || cmd.Stream == "" || cmd.NowMS <= 0 {
		return errors.New("storage: intake batch requires project, stream and timestamp")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range cmd.Items {
		if item.IssueID == "" || item.EventID == "" || item.IssueDigest == "" || item.RawDigest == "" || item.ObservedAtMS <= 0 {
			return errors.New("storage: intake item is missing identity or receipt facts")
		}
		var receiptID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM forge_event_receipts WHERE project_id=? AND forge_event_id=?`, cmd.ProjectID, item.EventID).Scan(&receiptID)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		intakeID := newID()
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT id FROM intake_items WHERE forge_kind=? AND normalized_host=? AND forge_project_key=? AND issue_id=?`, item.ForgeKind, item.Host, item.ProjectKey, item.IssueID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO intake_items
				(id,project_id,forge_kind,normalized_host,forge_project_key,issue_id,issue_url,issue_digest,state,version,created_at_ms,updated_at_ms)
				VALUES (?,?,?,?,?,?,?,?,'pending_evaluation',1,?,?)`, intakeID, cmd.ProjectID, item.ForgeKind, item.Host, item.ProjectKey, item.IssueID, item.IssueURL, item.IssueDigest, cmd.NowMS, cmd.NowMS); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			intakeID = existing
		}
		domainEventID := newID()
		payload, _ := json.Marshal(map[string]any{"forge_event_id": item.EventID, "event_kind": item.EventKind, "target_id": item.IssueID})
		if _, err = tx.ExecContext(ctx, `INSERT INTO events (id,project_id,type,source,actor,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES (?,?, 'intake.issue_observed','forge',?,1,?,?,?)`, domainEventID, cmd.ProjectID, nullable(item.Actor), string(payload), item.ObservedAtMS, cmd.NowMS); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO forge_event_receipts (id,project_id,forge_event_id,event_kind,target_kind,target_id,actor,raw_digest,disposition,domain_event_id,observed_at_ms) VALUES (?,?,?,?,?,?,?,?, 'accepted',?,?)`, newID(), cmd.ProjectID, item.EventID, valueOr(item.TargetKind, "issue"), item.IssueID, nullable(item.Actor), item.RawDigest, domainEventID, item.ObservedAtMS); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO forge_cursors(project_id,stream,cursor,poll_mode,next_poll_at_ms,updated_at_ms) VALUES(?,?,?,'active',?,?) ON CONFLICT(project_id,stream) DO UPDATE SET cursor=excluded.cursor,updated_at_ms=excluded.updated_at_ms`, cmd.ProjectID, cmd.Stream, nullable(cmd.Cursor), cmd.NowMS, cmd.NowMS); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

type IntakeDecisionCmd struct {
	IntakeID, AssessmentID, LogicalCallID string
	ExpectedVersion                       int
	Disposition                           string
	QuestionsJSON                         string
	PossibleDuplicateRunID                *string
	Rationale                             string
	NowMS                                 int64
	RunID                                 string
}

// PersistIntakeDecision is the single transaction that records a T1 result,
// advances the intake CAS, and creates the required clarification operation or
// forge Run. It deliberately does not perform Forge or provider I/O.
func (d *DB) PersistIntakeDecision(ctx context.Context, cmd IntakeDecisionCmd) error {
	if cmd.IntakeID == "" || cmd.AssessmentID == "" || cmd.LogicalCallID == "" || cmd.ExpectedVersion < 1 || cmd.NowMS <= 0 {
		return errors.New("storage: incomplete intake decision")
	}
	if cmd.Disposition != "ready" && cmd.Disposition != "needs_clarification" && cmd.Disposition != "possible_duplicate" {
		return errors.New("storage: invalid intake disposition")
	}
	if cmd.QuestionsJSON == "" {
		cmd.QuestionsJSON = "[]"
	}
	if !json.Valid([]byte(cmd.QuestionsJSON)) {
		return errors.New("storage: invalid intake questions JSON")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var projectID, kind, host, projectKey, issueID, issueURL, state string
	var version, generation int
	err = tx.QueryRowContext(ctx, `SELECT project_id,forge_kind,normalized_host,forge_project_key,issue_id,issue_url,state,version,clarification_generation FROM intake_items WHERE id=?`, cmd.IntakeID).Scan(&projectID, &kind, &host, &projectKey, &issueID, &issueURL, &state, &version, &generation)
	if err != nil {
		return err
	}
	if version != cmd.ExpectedVersion {
		return ErrRejectedStale
	}
	if state != "pending_evaluation" && state != "evaluating" {
		return fmt.Errorf("storage: intake state %q cannot accept decision", state)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO intake_assessments(id,intake_id,logical_call_id,disposition,questions_json,possible_duplicate_run_id,rationale,created_at_ms) VALUES(?,?,?,?,?,?,?,?)`, cmd.AssessmentID, cmd.IntakeID, cmd.LogicalCallID, cmd.Disposition, cmd.QuestionsJSON, nullableStringPtr(cmd.PossibleDuplicateRunID), cmd.Rationale, cmd.NowMS); err != nil {
		return err
	}
	newState := "ready"
	newGeneration := generation
	if cmd.Disposition == "needs_clarification" {
		newState = "awaiting_clarification"
		newGeneration++
	}
	if cmd.Disposition == "possible_duplicate" {
		newState = "awaiting_duplicate_confirmation"
		newGeneration++
	}
	if cmd.Disposition == "ready" {
		if cmd.RunID == "" {
			cmd.RunID = newID()
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO runs(id,source_kind,project_id,config_snapshot_id,forge_kind,forge_host,forge_project_key,issue_id,issue_url,status,max_attempts,created_at_ms,updated_at_ms) SELECT ?, 'forge',project_id,(SELECT config_snapshot_id FROM projects WHERE id=project_id),forge_kind,normalized_host,forge_project_key,issue_id,issue_url,'queued',3,?,? FROM intake_items WHERE id=?`, cmd.RunID, cmd.NowMS, cmd.NowMS, cmd.IntakeID); err != nil {
			return err
		}
		newState = "consumed"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intake_items SET state=?,version=version+1,latest_assessment_id=?,linked_run_id=?,duplicate_candidate_run_id=?,clarification_generation=?,updated_at_ms=? WHERE id=? AND version=?`, newState, cmd.AssessmentID, nullable(cmd.RunID), nullableStringPtr(cmd.PossibleDuplicateRunID), newGeneration, cmd.NowMS, cmd.IntakeID, cmd.ExpectedVersion); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"intake_id": cmd.IntakeID, "disposition": cmd.Disposition, "generation": newGeneration})
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?, 'intake.decision','brain',1,?,?,?)`, newID(), projectID, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
		return err
	}
	if cmd.Disposition != "ready" {
		purpose := "intake-clarification"
		if cmd.Disposition == "possible_duplicate" {
			purpose = "intake-duplicate-confirmation"
		}
		body, _ := json.Marshal(map[string]any{"forge_kind": kind, "forge_host": host, "forge_project_key": projectKey, "target_kind": "issue", "target_id": issueID, "purpose": purpose, "intake_id": cmd.IntakeID, "generation": newGeneration, "markdown": cmd.Rationale})
		op := Operation{Key: CommentOperationKey(purpose, cmd.IntakeID, newGeneration), Kind: OperationForgeComment, Payload: body}
		if err = insertOperation(ctx, tx, op, "", "", cmd.NowMS); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableStringPtr(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
