package channelworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) (string, error)

func (f resolverFunc) Resolve(ctx context.Context, ref string) (string, error) { return f(ctx, ref) }

type senderFunc func(context.Context, string, string) (string, error)

func (f senderFunc) Send(ctx context.Context, endpoint, body string) (string, error) {
	return f(ctx, endpoint, body)
}

func TestWebhookAdapterAcceptsStorageExactBatchFixtureAndReplay(t *testing.T) {
	payload := []byte(`{"batch_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcHJvamVjdC1h:issue:NDI","batch_kind":"daily_summary","channel":{"capabilities":["text"],"id":"ops-slack","renderer":"plain-v1","target_ref":"secret_ref:SIFT_CHANNEL_OPS_SLACK","type":"webhook"},"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcHJvamVjdC1h:issue:NDI:publish:1","delivery_kind":"attention_batch","due_at_ms":1785286800000,"forge_alert_target":{"forge_host":"github.com","forge_kind":"github","forge_project_key":"owner/project-a","target_id":"42","target_kind":"issue"},"members":[{"command_lines":[],"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcHJvamVjdC1h:issue:NDI:i-a","headline":"Agent 需要你澄清","interrupt_id":"i-a","interrupt_version":2,"links":[],"nonce":"n-a","options":[],"reason":"agent_blocked","severity":"high"},{"command_lines":[],"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcHJvamVjdC1h:issue:NDI:i-b","headline":"变更等待代码审阅","interrupt_id":"i-b","interrupt_version":2,"links":[],"nonce":"n-b","options":[],"reason":"code_review","severity":"high"}],"project_id":"project-a","rendered_text":"i-a: Agent 需要你澄清；i-b: 变更等待代码审阅","scope":"day","scope_id":"Asia/Shanghai:1785286800000"}`)
	sum := sha256.Sum256(payload)
	if got := hex.EncodeToString(sum[:]); got != "ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e" {
		t.Fatalf("fixture digest = %s", got)
	}
	key := "attention-batch:daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcHJvamVjdC1h:issue:NDI:publish:1"
	adapter := WebhookAdapter{
		Resolver: resolverFunc(func(_ context.Context, ref string) (string, error) {
			if ref != "SIFT_CHANNEL_OPS_SLACK" {
				t.Fatalf("ref = %q", ref)
			}
			return "https://example.test/hook?token=secret", nil
		}),
		Sender: senderFunc(func(_ context.Context, endpoint, body string) (string, error) {
			if !strings.Contains(body, "[sift "+key+"]") {
				t.Fatalf("marker absent: %q", body)
			}
			return "remote-1", nil
		}),
	}
	if _, err := adapter.Publish(context.Background(), payload, key); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Publish(context.Background(), payload, key); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookAdapterRejectsUnknownAndDoesNotLeakSenderError(t *testing.T) {
	payload := []byte(`{"delivery_kind":"interrupt","delivery_id":"interrupt:i:0:ops","interrupt_id":"i","escalation_no":0,"priority":"normal","interrupt_version":1,"nonce":"n","channel":{"id":"ops","type":"webhook","target_ref":"secret_ref:X","capabilities":["text"],"renderer":"plain-v1"},"rendered_text":"x","unexpected":true}`)
	adapter := WebhookAdapter{Resolver: resolverFunc(func(context.Context, string) (string, error) { return "https://example.test/?token=secret", nil }), Sender: senderFunc(func(context.Context, string, string) (string, error) {
		return "", errors.New("https://example.test/?token=secret")
	})}
	if _, err := adapter.Publish(context.Background(), payload, "k"); !errors.Is(err, ErrContractViolation) {
		t.Fatalf("unknown payload error = %v", err)
	}
	payload = []byte(strings.Replace(string(payload), `,"unexpected":true`, ``, 1))
	if _, err := adapter.Publish(context.Background(), payload, "k"); !errors.Is(err, ErrTransient) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("sender error = %v", err)
	}
}

func TestHTTPWebhookSenderHTTPDateRetryAfterUsesInjectedClock(t *testing.T) {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", base.Add(2*time.Second).Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	sender := HTTPWebhookSender{Client: srv.Client(), Now: func() time.Time { return base }}
	_, err := sender.Send(context.Background(), srv.URL, "x")
	var limited RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfterMS != 2000 {
		t.Fatalf("retry-after = %v, want 2000ms", err)
	}
}
