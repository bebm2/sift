package gate

import (
	"testing"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

func TestMergeConflictInterruptUsesCanonicalConflictDigest(t *testing.T) {
	in := input(t)
	in.Identity.ChangeID = "change-01"
	in.Change.HeadSHA = "0123456789012345678901234567890123456789"
	cmd, err := interruptCommand(storage.GateCandidate{RunID: "run", Version: 1}, in, Verdict{Code: "merge_conflict"}, config.Attention{}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd.Generation.ConflictDigest, storage.MergeConflictDigest(in.Identity.ChangeID, in.Change.HeadSHA); got != want {
		t.Fatalf("conflict digest = %s, want %s", got, want)
	}
}
