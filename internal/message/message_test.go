package message

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateCommandJSONRoundTrip(t *testing.T) {
	original := UpdateCommand{
		OperationID:  "op-1",
		Service:      "data-service",
		NodeInstance: "instance-1",
		FileURL:      "http://entry/internal/operations/op-1/file",
		FileName:     "config.yaml",
		FileSize:     1024,
		FileSHA256:   "abc123",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded UpdateCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round trip = %+v, want %+v", decoded, original)
	}
}

func TestUpdateResultOmitsEmptyError(t *testing.T) {
	data, err := json.Marshal(UpdateResult{OperationID: "op-1", Status: "RUNNING"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := raw["error"]; ok {
		t.Errorf("expected error field to be omitted, got %s", data)
	}
}

func TestNodeMessageJSONFields(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "register",
			value: NodeRegister{NodeID: "n1", InstanceID: "i1", Address: "n1:8080", Version: "dev", StartedAt: "2026-01-01T00:00:00Z"},
			want:  `"node_id":"n1"`,
		},
		{
			name:  "heartbeat",
			value: NodeHeartbeat{NodeID: "n1", InstanceID: "i1"},
			want:  `"instance_id":"i1"`,
		},
		{
			name:  "goodbye",
			value: NodeGoodbye{NodeID: "n1", InstanceID: "i1"},
			want:  `"node_id":"n1"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("json %s does not contain %s", data, tc.want)
			}
		})
	}
}
