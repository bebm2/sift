package controlplane

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/decode"
)

func TestV10aEndpointCapabilitiesAndSockets(t *testing.T) {
	home := testHome(t)
	s, err := Start(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx) }()
	waitSocket(t, filepath.Join(home.Path, "siftd.sock"))
	for _, name := range []string{"siftd.sock", "run.sock"} {
		info, err := os.Stat(filepath.Join(home.Path, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 0600", name, info.Mode().Perm())
		}
	}
	// No token is never accepted for an operator endpoint.
	response := call(t, filepath.Join(home.Path, "siftd.sock"), Request{ProtocolMajor: 1, ProtocolMinor: 0, ClientVersion: Version, RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.doctor", Auth: Auth{Kind: "operator"}, Params: map[string]any{}})
	if response.Error == nil || response.Error.Code != "unauthorized" {
		t.Fatalf("missing token response = %#v", response)
	}
	// Operator methods do not exist on run.sock, even with a syntactically valid token.
	response = call(t, filepath.Join(home.Path, "run.sock"), Request{ProtocolMajor: 1, ProtocolMinor: 0, ClientVersion: Version, RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.doctor", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{}})
	if response.Error == nil || response.Error.Code != "unknown_method" {
		t.Fatalf("ops on run socket = %#v", response)
	}
	// A run token cannot claim wrapper handoff authority.
	response = call(t, filepath.Join(home.Path, "run.sock"), Request{ProtocolMajor: 1, ProtocolMinor: 0, ClientVersion: Version, RequestID: "0123456789abcdef0123456789abcdef", Method: "claim.acquire", Auth: Auth{Kind: "run_token", Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Params: map[string]any{}})
	if response.Error == nil || response.Error.Code != "unauthorized" {
		t.Fatalf("run token claim = %#v", response)
	}
	response = call(t, filepath.Join(home.Path, "siftd.sock"), Request{ProtocolMajor: 1, ProtocolMinor: 0, ClientVersion: Version, RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.doctor", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{}})
	if !response.OK {
		t.Fatalf("valid operator doctor = %#v", response)
	}
	if response.Result.(map[string]any)["security_posture"] != "unsafe-local" {
		t.Fatal("doctor did not report unsafe-local")
	}
}

func TestSecondDaemonRefusesLock(t *testing.T) {
	home := testHome(t)
	s, err := Start(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := Start(home); err == nil {
		t.Fatal("second daemon unexpectedly started")
	}
}

func testHome(t *testing.T) config.Home {
	t.Helper()
	path, err := os.MkdirTemp("", "sift-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return config.Home{Path: path}
}

func call(t *testing.T, path string, request Request) Response {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := writeFrame(c, request); err != nil {
		t.Fatal(err)
	}
	b, err := readFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := decode.Decode(b, &response, decode.Closed); err != nil {
		t.Fatal(err)
	}
	return response
}
func waitSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("socket %s not created", path)
}
