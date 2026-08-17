package controlplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCommandCheckAuthStatusNeverPersistsRawOutput(t *testing.T) {
	const secretLike = "ghp_not-a-real-token"
	check := commandCheck(context.Background(), "forge-cli:project:login", "/bin/sh", []string{"-c", "echo version; echo " + secretLike})
	if check.Level != "ok" || check.Details["authenticated"] != true {
		t.Fatalf("auth check = %#v", check)
	}
	if _, ok := check.Details["output"]; ok {
		t.Fatalf("auth check retained raw output: %#v", check.Details)
	}
	body, err := json.Marshal(check)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secretLike) {
		t.Fatalf("doctor JSON leaks auth output: %s", body)
	}
}

func TestSafeProbeOutputUsesOneBoundedLine(t *testing.T) {
	if got := safeProbeOutput([]byte("  pi 1.2.3  \nsecond line\n")); got != "pi 1.2.3" {
		t.Fatalf("safeProbeOutput = %q", got)
	}
	long := strings.Repeat("x", 161)
	if got := safeProbeOutput([]byte(long)); !strings.HasSuffix(got, "…") || len([]rune(got)) != 161 {
		t.Fatalf("long output = %q", got)
	}
}
