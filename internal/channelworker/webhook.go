// Package channelworker contains the side-effecting consumers for Channel
// operations. It intentionally has no storage write port beyond completing an
// outbox attempt.
package channelworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/miaoxiaoyong/sift/internal/storage"
)

var (
	ErrAuthOrCapability  = errors.New("channel: auth or capability")
	ErrContractViolation = errors.New("channel: contract violation")
	ErrTransient         = errors.New("channel: transient")
	ErrRateLimited       = errors.New("channel: rate limited")
)

type SecretResolver interface {
	Resolve(context.Context, string) (string, error)
}
type WebhookSender interface {
	Send(context.Context, string, string) (string, error)
}

type webhookPayload struct {
	DeliveryKind     string `json:"delivery_kind"`
	DeliveryID       string `json:"delivery_id"`
	InterruptID      string `json:"interrupt_id"`
	EscalationNo     int    `json:"escalation_no"`
	Priority         string `json:"priority"`
	InterruptVersion int    `json:"interrupt_version"`
	Nonce            string `json:"nonce"`
	BatchID          string `json:"batch_id"`
	BatchKind        string `json:"batch_kind"`
	ProjectID        string `json:"project_id"`
	Scope            string `json:"scope"`
	ScopeID          string `json:"scope_id"`
	DueAtMS          int64  `json:"due_at_ms"`
	Channel          struct {
		ID           string   `json:"id"`
		Type         string   `json:"type"`
		TargetRef    string   `json:"target_ref"`
		Renderer     string   `json:"renderer"`
		Capabilities []string `json:"capabilities"`
	} `json:"channel"`
	RenderedText string            `json:"rendered_text"`
	Members      []json.RawMessage `json:"members"`
}

// WebhookAdapter executes exactly one attempt. It resolves only the sealed
// secret_ref handle; endpoint values never enter evidence or diagnostics.
type WebhookAdapter struct {
	Resolver SecretResolver
	Sender   WebhookSender
}

func (a WebhookAdapter) Publish(ctx context.Context, payload []byte, operationKey string) (json.RawMessage, error) {
	var p webhookPayload
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrContractViolation)
	}
	if p.DeliveryKind != "interrupt" && p.DeliveryKind != "attention_batch" || p.DeliveryID == "" || p.Channel.Type != "webhook" || p.Channel.Renderer != "plain-v1" {
		return nil, fmt.Errorf("%w: closed channel_publish payload", ErrContractViolation)
	}
	if p.DeliveryKind == "attention_batch" && len(p.Members) == 0 {
		return nil, fmt.Errorf("%w: empty batch", ErrContractViolation)
	}
	const prefix = "secret_ref:"
	if !strings.HasPrefix(p.Channel.TargetRef, prefix) || len(p.Channel.TargetRef) == len(prefix) || strings.ContainsAny(p.Channel.TargetRef[len(prefix):], "\r\n") {
		return nil, fmt.Errorf("%w: target_ref", ErrContractViolation)
	}
	if a.Resolver == nil {
		return nil, ErrAuthOrCapability
	}
	endpoint, err := a.Resolver.Resolve(ctx, p.Channel.TargetRef[len(prefix):])
	if err != nil {
		return nil, fmt.Errorf("%w: resolver rejected handle", ErrAuthOrCapability)
	}
	parsed, parseErr := url.Parse(endpoint)
	if endpoint == "" || strings.ContainsAny(endpoint, "\r\n") || parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%w: resolved endpoint", ErrContractViolation)
	}
	if a.Sender == nil {
		return nil, ErrAuthOrCapability
	}
	body := p.RenderedText + "\n[sift " + operationKey + "]"
	remote, err := a.Sender.Send(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(fmt.Sprintf(`{"remote_ref":%q}`, remote)), nil
}

type Worker struct {
	DB         *storage.DB
	Adapter    WebhookAdapter
	Now        func() int64
	LeaseMS    int64
	WorkerID   string
	AlertAfter int
}

func (w *Worker) RunOnce(ctx context.Context) error {
	now := int64(1)
	if w.Now != nil {
		now = w.Now()
	}
	claim, err := w.DB.ClaimOutboxOperationKind(ctx, w.WorkerID, storage.OperationChannelPublish, now, w.LeaseMS)
	if err != nil || claim == nil {
		return err
	}
	evidence, err := w.Adapter.Publish(ctx, claim.Payload, claim.Key)
	if err == nil {
		return w.DB.CompleteOutboxAttempt(ctx, *claim, storage.CompleteOutcome{State: storage.OperationSucceeded, Evidence: evidence, NowMS: now, ChannelFailureAlertAfter: w.AlertAfter})
	}
	state, class := storage.OperationRetryable, storage.ErrorTransient
	summary := err.Error()
	switch {
	case errors.Is(err, ErrAuthOrCapability):
		state, class = storage.OperationFailed, storage.ErrorAuthCapability
	case errors.Is(err, ErrContractViolation):
		state, class = storage.OperationFailed, storage.ErrorContract
	case errors.Is(err, ErrRateLimited):
		class = storage.ErrorRateLimited
	}
	return w.DB.CompleteOutboxAttempt(ctx, *claim, storage.CompleteOutcome{State: state, ErrorClass: class, ErrorSummary: summary, NowMS: now, Backoff: storage.BackoffPolicy{InitialDelayMS: 1000, MaxDelayMS: 60000, Multiplier: 2}, ChannelFailureAlertAfter: w.AlertAfter})
}
