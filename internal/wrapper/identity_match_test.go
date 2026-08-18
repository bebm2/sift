package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	runtimepkg "github.com/xsift/sift/internal/runtime"
)

// TestProductionWrapperPersistedIdentityMatchesPlatformInspector is the #1022
// launch path: real wrapper writes control.json, then the daemon inspector
// must match that identity. A mismatch is process_identity_unknown.
func TestProductionWrapperPersistedIdentityMatchesPlatformInspector(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("requires native process inspector")
	}
	wrapperPath := buildWrapper(t)
	root, runDir, bootstrap := validBootstrap(t, "/bin/sh", []string{"-c", "trap '' TERM; while :; do :; done"})
	server := newWrapperServer(t, root, "")
	defer server.Close()
	cmd := osexec.Command(wrapperPath, bootstrap)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	id := waitPersistedWrapperIdentity(t, filepath.Join(runDir, "control.json"))
	got, err := (runtimepkg.PlatformProcessInspector{}).Observe(context.Background(), id)
	if err != nil || !got.Exists {
		t.Fatalf("observe = %#v, %v", got, err)
	}
	if !runtimepkg.SameIdentity(id, got.ProcessIdentity) {
		t.Fatalf("persisted %#v vs observe %#v", id, got.ProcessIdentity)
	}

	term := runtimepkg.Terminator{
		Inspector: runtimepkg.PlatformProcessInspector{},
		Signaler:  runtimepkg.UnixProcessSignaler{},
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}
	result, err := term.Terminate(context.Background(), id, runtimepkg.TerminationConfig{
		AbsenceRechecks: 3, RecheckInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cause == runtimepkg.TerminationIdentityUnknown {
		t.Fatal("terminator returned process_identity_unknown for a live matching wrapper")
	}
	_ = cmd.Wait()
}

func waitPersistedWrapperIdentity(t *testing.T, controlPath string) runtimepkg.ProcessIdentity {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(controlPath)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var control struct {
			ControlNonce    string `json:"control_nonce"`
			WrapperIdentity struct {
				PID         int    `json:"pid"`
				StartedAtMS int64  `json:"started_at_ms"`
				Executable  string `json:"executable"`
				PGID        int    `json:"pgid"`
			} `json:"wrapper_identity"`
		}
		if json.Unmarshal(data, &control) != nil || control.ControlNonce == "" ||
			control.WrapperIdentity.PID <= 0 || control.WrapperIdentity.StartedAtMS <= 0 ||
			control.WrapperIdentity.Executable == "" || control.WrapperIdentity.PGID <= 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		digest := sha256.Sum256([]byte(control.ControlNonce))
		return runtimepkg.ProcessIdentity{
			PID:              control.WrapperIdentity.PID,
			StartedAtMS:      control.WrapperIdentity.StartedAtMS,
			Executable:       control.WrapperIdentity.Executable,
			PGID:             control.WrapperIdentity.PGID,
			ControlNonceHash: hex.EncodeToString(digest[:]),
			ControlPath:      controlPath,
		}
	}
	t.Fatal("wrapper did not persist complete identity")
	return runtimepkg.ProcessIdentity{}
}
