package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoctorDefaultSummaryHidesImplementationNoise(t *testing.T) {
	_ = freshHome(t)
	result := map[string]any{
		"offline": false, "exit_code": 2,
		"checks": []any{
			map[string]any{"id": "agent-cli:pi", "level": "ok", "message": "command is available", "details": map[string]any{"output": "version\nsecret-like-second-line"}},
			map[string]any{"id": "policy:stale", "level": "error", "message": "exit status 128", "details": map[string]any{}},
			map[string]any{"id": "outbox:backlog", "level": "warning", "message": "outbox operations remain pending", "details": map[string]any{}},
			map[string]any{"id": "process-group:pi", "level": "warning", "message": "process-group qualification is not verified", "details": map[string]any{}},
			map[string]any{"id": "tm6:sift-home", "level": "warning", "message": "same UID", "details": map[string]any{}},
		},
	}
	var out bytes.Buffer
	renderDoctorWithOptions(&out, result, doctorOptions{})
	for _, want := range []string{"Sift 状态：需要处理", "其他已登记项目", "本地仓库不可读", "有待处理的投递操作", "完整安全检查"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, out.String())
		}
	}
	for _, forbidden := range []string{"agent-cli:pi", "secret-like-second-line", "process-group:pi", "tm6:sift-home"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("summary leaked implementation detail %q:\n%s", forbidden, out.String())
		}
	}
}

func TestDoctorDetailsAndDebugAreExplicit(t *testing.T) {
	result := map[string]any{
		"offline": false, "exit_code": 1,
		"stage_ms": map[string]any{"exec": 123, "sqlite": 4},
		"checks":   []any{map[string]any{"id": "agent-cli:pi", "level": "ok", "message": "command is available", "details": map[string]any{"path": "/bin/pi", "output": "pi 1.0"}}},
	}
	var details, debug bytes.Buffer
	renderDoctorWithOptions(&details, result, doctorOptions{details: true})
	renderDoctorWithOptions(&debug, result, doctorOptions{details: true, debug: true})
	if !strings.Contains(details.String(), "agent-cli:pi") || strings.Contains(details.String(), "stage_ms") {
		t.Fatalf("details output = %q", details.String())
	}
	if !strings.Contains(debug.String(), "agent-cli:pi") || !strings.Contains(debug.String(), "stage_ms exec=123ms sqlite=4ms") {
		t.Fatalf("debug output = %q", debug.String())
	}
}

func TestParseDoctorOptionsSeparatesHumanAndJSONModes(t *testing.T) {
	for _, tc := range []struct {
		args []string
		ok   bool
		want doctorOptions
	}{
		{[]string{"--details"}, true, doctorOptions{details: true}},
		{[]string{"--debug", "--offline"}, true, doctorOptions{offline: true, details: true, debug: true}},
		{[]string{"--json", "--details"}, false, doctorOptions{}},
		{[]string{"--unknown"}, false, doctorOptions{}},
	} {
		got, ok := parseDoctorOptions(tc.args)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("parseDoctorOptions(%v) = %#v, %v; want %#v, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
}
