package event

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	Sequence  uint64          `json:"sequence"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Actor     string          `json:"actor"`
	Data      json.RawMessage `json:"data,omitempty"`
}
