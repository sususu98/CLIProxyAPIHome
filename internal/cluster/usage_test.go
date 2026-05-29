package cluster

import "testing"

func TestUsageRecordFromPayloadStoresRequestIDAndHomeIP(t *testing.T) {
	payload := `{"timestamp":"2026-05-29T01:02:03Z","request_id":"req-usage-1","tokens":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`

	record, errRecord := UsageRecordFromPayload(payload, "192.0.2.10")
	if errRecord != nil {
		t.Fatalf("UsageRecordFromPayload: %v", errRecord)
	}

	if record.RequestID != "req-usage-1" {
		t.Fatalf("request id = %q, want req-usage-1", record.RequestID)
	}
	if record.HomeIP != "192.0.2.10" {
		t.Fatalf("home ip = %q, want 192.0.2.10", record.HomeIP)
	}
	if record.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want 15", record.TotalTokens)
	}
}
