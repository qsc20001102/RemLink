package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"remlink/internal/model"
)

var ErrSessionNotFound = errors.New("Session not found")

func (s *Store) CreateSession(ctx context.Context, session model.Session) error {
	if session.ID == 0 || session.EngineerNodeID == "" || session.SiteNodeID == "" || !session.Status.Valid() || len(session.CIDRs) == 0 {
		return errors.New("Session requires ID, participants, valid status, and CIDRs")
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create Session: %w", err)
	}
	defer tx.Rollback()
	id := strconv.FormatUint(session.ID, 10)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions(session_id, engineer_node_id, site_node_id, status, created_at, error_code)
		VALUES(?, ?, ?, ?, ?, NULL)
	`, id, session.EngineerNodeID, session.SiteNodeID, session.Status, formatTime(session.CreatedAt)); err != nil {
		return fmt.Errorf("insert Session %s: %w", id, err)
	}
	for _, prefix := range session.CIDRs {
		if !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
			return fmt.Errorf("invalid Session CIDR %s", prefix)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_cidrs(session_id, cidr) VALUES(?, ?)`, id, prefix.String()); err != nil {
			return fmt.Errorf("insert Session CIDR: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_stats(session_id, updated_at) VALUES(?, ?)
	`, id, formatTime(session.CreatedAt)); err != nil {
		return fmt.Errorf("initialize Session stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create Session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, sessionID uint64) (model.Session, error) {
	id := strconv.FormatUint(sessionID, 10)
	var (
		session                       model.Session
		status, createdAt             string
		activeAt, closedAt, errorCode sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT s.engineer_node_id, s.site_node_id, s.status, s.created_at, s.active_at, s.closed_at, s.error_code,
		       st.tx_bytes, st.rx_bytes, st.tx_packets, st.rx_packets
		FROM sessions s JOIN session_stats st ON st.session_id = s.session_id
		WHERE s.session_id = ?
	`, id).Scan(&session.EngineerNodeID, &session.SiteNodeID, &status, &createdAt, &activeAt, &closedAt, &errorCode,
		&session.Counters.UploadBytes, &session.Counters.DownloadBytes,
		&session.Counters.UploadPackets, &session.Counters.DownloadPackets)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, fmt.Errorf("%w: %d", ErrSessionNotFound, sessionID)
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("get Session %d: %w", sessionID, err)
	}
	session.ID = sessionID
	session.Status = model.SessionStatus(status)
	if !session.Status.Valid() {
		return model.Session{}, fmt.Errorf("invalid stored Session status %q", status)
	}
	session.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return model.Session{}, err
	}
	if activeAt.Valid {
		value, err := parseTime(activeAt.String)
		if err != nil {
			return model.Session{}, err
		}
		session.ActiveAt = &value
	}
	if closedAt.Valid {
		value, err := parseTime(closedAt.String)
		if err != nil {
			return model.Session{}, err
		}
		session.ClosedAt = &value
	}
	if errorCode.Valid {
		session.ErrorCode = errorCode.String
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cidr FROM session_cidrs WHERE session_id = ? ORDER BY cidr`, id)
	if err != nil {
		return model.Session{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return model.Session{}, err
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return model.Session{}, err
		}
		session.CIDRs = append(session.CIDRs, prefix)
	}
	return session, rows.Err()
}

// SessionFilter limits Admin session queries without loading the full history.
type SessionFilter struct {
	Kind           string
	SessionID      string
	EngineerNodeID string
	SiteNodeID     string
	Status         string
	From           time.Time
	To             time.Time
	Limit          int
}

// ListSessions returns complete Sessions newest first for callers that need all records.
func (s *Store) ListSessions(ctx context.Context) ([]model.Session, error) {
	return s.ListSessionsFiltered(ctx, SessionFilter{})
}

// ListSessionsFiltered applies optional filters before loading Session details.
func (s *Store) ListSessionsFiltered(ctx context.Context, filter SessionFilter) ([]model.Session, error) {
	var query strings.Builder
	query.WriteString(`SELECT session_id FROM sessions WHERE 1=1`)
	args := make([]any, 0, 8)
	switch filter.Kind {
	case "live":
		query.WriteString(` AND status NOT IN ('CLOSED','FAILED')`)
	case "history":
		query.WriteString(` AND status IN ('CLOSED','FAILED')`)
	}
	if filter.SessionID != "" {
		query.WriteString(` AND session_id = ?`)
		args = append(args, filter.SessionID)
	}
	if filter.EngineerNodeID != "" {
		query.WriteString(` AND engineer_node_id = ?`)
		args = append(args, filter.EngineerNodeID)
	}
	if filter.SiteNodeID != "" {
		query.WriteString(` AND site_node_id = ?`)
		args = append(args, filter.SiteNodeID)
	}
	if filter.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, filter.Status)
	}
	if !filter.From.IsZero() {
		query.WriteString(` AND julianday(COALESCE(closed_at, created_at)) >= julianday(?)`)
		args = append(args, formatTime(filter.From))
	}
	if !filter.To.IsZero() {
		query.WriteString(` AND julianday(COALESCE(closed_at, created_at)) <= julianday(?)`)
		args = append(args, formatTime(filter.To))
	}
	if filter.Kind == "history" {
		query.WriteString(` ORDER BY julianday(COALESCE(closed_at, created_at)) DESC, session_id DESC`)
	} else {
		query.WriteString(` ORDER BY created_at DESC, session_id DESC`)
	}
	if filter.Limit > 0 {
		query.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list Session IDs: %w", err)
	}
	var ids []uint64
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sessions := make([]model.Session, 0, len(ids))
	for _, id := range ids {
		session, err := s.GetSession(ctx, id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s *Store) UpdateSessionStatus(ctx context.Context, sessionID uint64, status model.SessionStatus, errorCode string) error {
	if !status.Valid() {
		return fmt.Errorf("invalid Session status %q", status)
	}
	now := formatTime(time.Now().UTC())
	activeAt, closedAt := any(nil), any(nil)
	if status == model.SessionActive {
		activeAt = now
	}
	if status == model.SessionClosed || status == model.SessionFailed {
		closedAt = now
	}
	var errorValue any
	if errorCode != "" {
		errorValue = errorCode
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, active_at = COALESCE(?, active_at),
		closed_at = COALESCE(?, closed_at), error_code = ? WHERE session_id = ?
	`, status, activeAt, closedAt, errorValue, strconv.FormatUint(sessionID, 10))
	if err != nil {
		return fmt.Errorf("update Session %d status: %w", sessionID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %d", ErrSessionNotFound, sessionID)
	}
	return nil
}

func (s *Store) UpdateSessionStats(ctx context.Context, sessionID uint64, counters model.SessionCounters) error {
	if counters.UploadBytes > math.MaxInt64 || counters.DownloadBytes > math.MaxInt64 ||
		counters.UploadPackets > math.MaxInt64 || counters.DownloadPackets > math.MaxInt64 {
		return errors.New("Session counters exceed SQLite INTEGER capacity")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE session_stats SET tx_bytes = ?, rx_bytes = ?, tx_packets = ?, rx_packets = ?, updated_at = ?
		WHERE session_id = ?
	`, counters.UploadBytes, counters.DownloadBytes, counters.UploadPackets, counters.DownloadPackets,
		formatTime(time.Now().UTC()), strconv.FormatUint(sessionID, 10))
	if err != nil {
		return fmt.Errorf("update Session %d stats: %w", sessionID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %d", ErrSessionNotFound, sessionID)
	}
	return nil
}

// CloseOpenSessions enforces the v1 no-resume rule on Server startup.
func (s *Store) CloseOpenSessions(ctx context.Context) (int64, error) {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = 'CLOSED', closed_at = ?, error_code = NULL
		WHERE status NOT IN ('CLOSED', 'FAILED')
	`, now)
	if err != nil {
		return 0, fmt.Errorf("close open Sessions at startup: %w", err)
	}
	return result.RowsAffected()
}
