package cliops

const PeerMessageMaxRequest = 1 << 20
const PeerMessageMaxReply = 64 << 10
const peerMessageOutputBudget = 32 << 10

// PeerMessage carries the frozen request. The original source writers own all
// target, text, policy, hop and rate validation.
type PeerMessage struct {
	Bot  string `json:"bot"`
	Text string `json:"text"`
}

type PeerMessageReply struct {
	Bot           string `json:"bot,omitempty"`
	Route         string `json:"route"`
	Posted        bool   `json:"posted"`
	EventID       string `json:"event_id,omitempty"`
	EnvelopeID    string `json:"envelope_id,omitempty"`
	Gateway       string `json:"gateway,omitempty"`
	GatewayStatus int    `json:"gateway_status,omitempty"`
	Queued        bool   `json:"queued"`
	Stage         string `json:"stage"`
	Recorded      *bool  `json:"recorded,omitempty"`
	RecordSeq     int    `json:"record_seq,omitempty"`
	Error         string `json:"error,omitempty"`
}
