package storage

import (
	"context"
	"testing"
)

func TestChannelFailureAlertRequiresClosedPayload(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()

	for _, payload := range []string{
		`{}`,
		`{"forge_host":"github.com","forge_kind":"github","forge_project_key":"owner/repo","markdown":"x","purpose":"wrong","target_id":"1","target_kind":"issue"}`,
	} {
		_, err := db.EnqueueOperation(ctx, Operation{
			Key:     "alert:channel_failure:delivery:1",
			Kind:    OperationForgeAlert,
			Payload: []byte(payload),
		}, testNow)
		if err == nil {
			t.Fatalf("accepted non-closed channel alert payload %s", payload)
		}
	}

	if _, err := db.EnqueueOperation(ctx, Operation{
		Key:     "alert:channel_failure:delivery:1",
		Kind:    OperationForgeAlert,
		Payload: []byte(`{"forge_host":"github.com","forge_kind":"github","forge_project_key":"owner/repo","markdown":"[sift alert:channel_failure:delivery:1:1]","purpose":"channel_failure","target_id":"1","target_kind":"issue"}`),
	}, testNow); err != nil {
		t.Fatalf("accepted closed channel alert payload: %v", err)
	}
}
