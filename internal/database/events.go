package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"remlink/internal/logging"
	"remlink/internal/model"
)

func (s *Store) AppendEvent(ctx context.Context, event model.EventLog) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Level == "" || event.Module == "" || event.Message == "" {
		return errors.New("event level, module, and message are required")
	}
	module := logging.Module(strings.ToUpper(event.Module))
	if !module.Valid() {
		return fmt.Errorf("event module %q is outside the RemLink taxonomy", event.Module)
	}
	fields := event.FieldsJSON
	if len(fields) == 0 {
		fields = json.RawMessage(`{}`)
	}
	if !json.Valid(fields) {
		return errors.New("event fields must be valid JSON")
	}
	// Keep names as event-time snapshots so deleting or renaming a device does
	// not make earlier human-readable events anonymous or misleading.
	var namedFields map[string]any
	if json.Unmarshal(fields, &namedFields) == nil {
		if namedFields == nil {
			namedFields = make(map[string]any)
		}
		addName := func(id, key string) {
			if id == "" || namedFields[key] != nil {
				return
			}
			if node, err := s.GetNode(ctx, id); err == nil {
				namedFields[key] = node.Name
			}
		}
		addName(event.NodeID, "node_name")
		if event.SessionID != 0 && namedFields["engineer_node_id"] == nil {
			var engineer, site string
			if err := s.db.QueryRowContext(ctx, `SELECT engineer_node_id, site_node_id FROM sessions WHERE session_id = ?`, strconv.FormatUint(event.SessionID, 10)).Scan(&engineer, &site); err == nil {
				namedFields["engineer_node_id"], namedFields["site_node_id"] = engineer, site
			}
		}
		engineer, _ := namedFields["engineer_node_id"].(string)
		site, _ := namedFields["site_node_id"].(string)
		addName(engineer, "engineer_name")
		addName(site, "site_name")
		fields, _ = json.Marshal(namedFields)
	}
	var sessionID any
	if event.SessionID != 0 {
		sessionID = strconv.FormatUint(event.SessionID, 10)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO event_logs(time, level, module, node_id, session_id, message, fields_json)
		VALUES(?, ?, ?, NULLIF(?, ''), ?, ?, ?)
	`, formatTime(event.Time), strings.ToUpper(event.Level), string(module), event.NodeID, sessionID, event.Message, string(fields))
	return err
}

func (s *Store) ListEvents(ctx context.Context, filter model.EventLogFilter) ([]model.EventLog, error) {
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 200
	}
	query := `SELECT id, time, level, module, node_id, session_id, message, fields_json FROM event_logs WHERE 1=1`
	var arguments []any
	if filter.Level != "" {
		query += ` AND level = ?`
		arguments = append(arguments, strings.ToUpper(filter.Level))
	}
	if filter.Module != "" {
		query += ` AND module = ?`
		arguments = append(arguments, strings.ToUpper(filter.Module))
	}
	if filter.NodeID != "" {
		query += ` AND (node_id = ? OR json_extract(fields_json, '$.engineer_node_id') = ? OR json_extract(fields_json, '$.site_node_id') = ?)`
		arguments = append(arguments, filter.NodeID, filter.NodeID, filter.NodeID)
	}
	if filter.SessionID != 0 {
		query += ` AND session_id = ?`
		arguments = append(arguments, strconv.FormatUint(filter.SessionID, 10))
	}
	if !filter.From.IsZero() {
		query += ` AND time >= ?`
		arguments = append(arguments, formatTime(filter.From))
	}
	if !filter.To.IsZero() {
		query += ` AND time <= ?`
		arguments = append(arguments, formatTime(filter.To))
	}
	query += ` ORDER BY time DESC, id DESC LIMIT ?`
	arguments = append(arguments, filter.Limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.EventLog, 0)
	for rows.Next() {
		var event model.EventLog
		var rawTime, fields string
		var nodeID, sessionID sql.NullString
		if err := rows.Scan(&event.ID, &rawTime, &event.Level, &event.Module, &nodeID, &sessionID, &event.Message, &fields); err != nil {
			return nil, err
		}
		parsed, err := parseTime(rawTime)
		if err != nil {
			return nil, err
		}
		event.Time = parsed
		event.NodeID = nodeID.String
		if sessionID.Valid {
			event.SessionID, err = strconv.ParseUint(sessionID.String, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse event SessionID: %w", err)
			}
		}
		event.FieldsJSON = json.RawMessage(fields)
		events = append(events, event)
	}
	return events, rows.Err()
}
