package model

import (
	"encoding/json"
	"time"
)

type EventLog struct {
	ID         int64           `json:"id"`
	Time       time.Time       `json:"time"`
	Level      string          `json:"level"`
	Module     string          `json:"module"`
	NodeID     string          `json:"node_id,omitempty"`
	SessionID  uint64          `json:"session_id,omitempty,string"`
	Message    string          `json:"message"`
	FieldsJSON json.RawMessage `json:"fields"`
}

type EventLogFilter struct {
	Level     string
	Module    string
	NodeID    string
	SessionID uint64
	From      time.Time
	To        time.Time
	Limit     int
}
