package message

type NodeRegister struct {
	NodeID       string   `json:"node_id"`
	InstanceID   string   `json:"instance_id"`
	Address      string   `json:"address"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	StartedAt    string   `json:"started_at"`
}

type NodeHeartbeat struct {
	NodeID     string `json:"node_id"`
	InstanceID string `json:"instance_id"`
}

type NodeGoodbye struct {
	NodeID     string `json:"node_id"`
	InstanceID string `json:"instance_id"`
}
