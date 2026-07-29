package channelworker

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type resolverFunc func(context.Context, string) (string, error)

func (f resolverFunc) Resolve(ctx context.Context, ref string) (string, error) { return f(ctx, ref) }

type senderFunc func(context.Context, string, string) (string, error)

func (f senderFunc) Send(ctx context.Context, endpoint, body string) (string, error) {
	return f(ctx, endpoint, body)
}

func TestWebhookAdapterAcceptsClosedBatchFixture(t *testing.T) {
	payload := []byte(`{"batch_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:host:project:issue:NDI","batch_kind":"daily_summary","channel":{"capabilities":["text"],"id":"ops-slack","renderer":"plain-v1","target_ref":"secret_ref:SIFT_CHANNEL_OPS_SLACK","type":"webhook"},"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:host:project:issue:NDI:publish:1","delivery_kind":"attention_batch","due_at_ms":1785286800000,"forge_alert_target":{"forge_host":"github.com","forge_kind":"github","forge_project_key":"owner/project-a","target_id":"42","target_kind":"issue"},"members":[{"command_lines":[],"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:host:project:issue:NDI:i-a","headline":"a","interrupt_id":"i-a","interrupt_version":2,"links":[],"nonce":"n-a","options":[],"reason":"agent_blocked","severity":"high"},{"command_lines":[],"delivery_id":"daily:project-a:Asia/Shanghai:1785286800000:ops-slack:github:host:project:issue:NDI:i-b","headline":"b","interrupt_id":"i-b","interrupt_version":2,"links":[],"nonce":"n-b","options":[],"reason":"code_review","severity":"high"}],"project_id":"project-a","rendered_text":"a; b","scope":"day","scope_id":"Asia/Shanghai:1785286800000"}`)
	adapter := WebhookAdapter{
		Resolver: resolverFunc(func(_ context.Context, ref string) (string, error) {
			if ref != "SIFT_CHANNEL_OPS_SLACK" {
				t.Fatalf("ref = %q", ref)
			}
			return "https://example.test/hook?token=secret", nil
		}),
		Sender: senderFunc(func(_ context.Context, endpoint, body string) (string, error) {
			if !strings.Contains(body, "[sift attention-batch:test:publish:1]") {
				t.Fatalf("marker absent: %q", body)
			}
			return "remote-1", nil
		}),
	}
	if _, err := adapter.Publish(context.Background(), payload, "attention-batch:test:publish:1"); err != nil {
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
