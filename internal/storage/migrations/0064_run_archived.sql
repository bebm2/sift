-- Soft-delete (archive) support for runs, backing `sift rm`.
--
-- A run's audit trail is append-only (events, budget_entries, ledger_entries,
-- gate_evaluations, ...) and cannot be hard-deleted without orphaning those
-- immutable rows. `sift rm` therefore archives: it stamps archived_at_ms so the
-- run is hidden from `sift ps` (and `sift ps -a`) while every event, ledger
-- entry and metric is retained verbatim for timeline/metrics/audit. The column
-- is nullable; existing runs are not archived (NULL).
ALTER TABLE runs ADD COLUMN archived_at_ms INTEGER;
