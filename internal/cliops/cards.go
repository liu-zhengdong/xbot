package cliops

const CardsMaxRequest = 1 << 20
const CardsMaxReply = 64 << 10
const cardsOutputBudget = 32 << 10

// RestartCall keeps the original optional continuation hint for working Bots.
type RestartCall struct {
	Hint string `json:"hint,omitempty"`
}

// HTTPCall keeps the original request fields; an empty method means GET.
type HTTPCall struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// CardReply contains only the original text and the actual error.
type CardReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}
