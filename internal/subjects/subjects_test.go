package subjects

import "testing"

func TestCommandSubjectComposition(t *testing.T) {
	got := CommandPrefix + "node-1"
	if got != "update.command.node-1" {
		t.Fatalf("command subject = %q, want %q", got, "update.command.node-1")
	}
}

func TestSubjectValues(t *testing.T) {
	if Result != "update.result" {
		t.Errorf("Result = %q, want %q", Result, "update.result")
	}
	if NodeRegister != "node.register" {
		t.Errorf("NodeRegister = %q, want %q", NodeRegister, "node.register")
	}
	if NodeHeartbeat != "node.heartbeat" {
		t.Errorf("NodeHeartbeat = %q, want %q", NodeHeartbeat, "node.heartbeat")
	}
	if NodeGoodbye != "node.goodbye" {
		t.Errorf("NodeGoodbye = %q, want %q", NodeGoodbye, "node.goodbye")
	}
}
