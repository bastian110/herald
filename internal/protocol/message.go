package protocol

import "encoding/json"

// Envelope is the JSON line exchanged between herald and harnesses.
// op values: "send" (harness→herald), "message" (herald→harness),
//
//	"ack" (herald→harness after send), "error" (herald→harness on failure).
type Envelope struct {
	Op       string   `json:"op"`
	Kind     string   `json:"kind,omitempty"`
	Text     string   `json:"text,omitempty"`
	From     string   `json:"from,omitempty"`
	ChatID   int64    `json:"chat_id,omitempty"`
	FileID   string   `json:"file_id,omitempty"`
	FileIDs  []string `json:"file_ids,omitempty"`
	MimeType string   `json:"mime_type,omitempty"`
	OK       bool     `json:"ok,omitempty"`
	Error    string   `json:"message,omitempty"`
}

// Encode serialises e to JSON and appends a newline delimiter.
func Encode(e Envelope) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode deserialises a single JSON line (without trailing newline).
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	return e, json.Unmarshal(data, &e)
}
