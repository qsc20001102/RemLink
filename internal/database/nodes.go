package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"remlink/internal/model"
)

// ErrNodeNotFound is returned when a requested node does not exist.
var ErrNodeNotFound = errors.New("node not found")

// Store groups repositories backed by a migrated RemLink database.
type Store struct {
	db *sql.DB
}

// NewStore creates repositories over db. Open must have migrated db first.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB exposes the underlying connection for transaction-oriented services.
func (s *Store) DB() *sql.DB { return s.db }

// CreateNode persists a newly registered node.
func (s *Store) CreateNode(ctx context.Context, node model.Node) error {
	if err := validateNode(node); err != nil {
		return err
	}
	now := time.Now().UTC()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = node.CreatedAt
	}
	if node.Status == "" {
		node.Status = model.NodeOffline
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes(
			node_id, type, name, overlay_ip, wg_public_key, node_token_hash,
			status, version, os_version, last_seen, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, node.Type, node.Name, node.OverlayIP.String(), node.WGPublicKey,
		node.NodeTokenHash, node.Status, node.Version, node.OSVersion,
		nullTime(node.LastSeen), formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create node %q: %w", node.ID, err)
	}
	return nil
}

// GetNode returns a node by its stable UUID.
func (s *Store) GetNode(ctx context.Context, nodeID string) (model.Node, error) {
	row := s.db.QueryRowContext(ctx, nodeSelect+` WHERE node_id = ?`, nodeID)
	node, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Node{}, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	if err != nil {
		return model.Node{}, fmt.Errorf("get node %q: %w", nodeID, err)
	}
	return node, nil
}

// ListNodes returns all nodes in stable creation order.
func (s *Store) ListNodes(ctx context.Context) ([]model.Node, error) {
	rows, err := s.db.QueryContext(ctx, nodeSelect+` ORDER BY created_at, node_id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	nodes := make([]model.Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return nodes, nil
}

// ListOverlayIPs returns every reserved node address.
func (s *Store) ListOverlayIPs(ctx context.Context) ([]netip.Addr, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT overlay_ip FROM nodes ORDER BY overlay_ip`)
	if err != nil {
		return nil, fmt.Errorf("list overlay addresses: %w", err)
	}
	defer rows.Close()
	var addresses []netip.Addr
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan overlay address: %w", err)
		}
		address, err := netip.ParseAddr(raw)
		if err != nil || !address.Is4() {
			return nil, fmt.Errorf("invalid stored overlay address %q", raw)
		}
		addresses = append(addresses, address)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overlay addresses: %w", err)
	}
	return addresses, nil
}

// UpdateNodeRegistration rotates the token and refreshes mutable client data.
func (s *Store) UpdateNodeRegistration(ctx context.Context, node model.Node) error {
	if len(node.NodeTokenHash) == 0 {
		return errors.New("node token hash must not be empty")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET name = ?, node_token_hash = ?, version = ?, os_version = ?, updated_at = ?
		WHERE node_id = ?
	`, node.Name, node.NodeTokenHash, node.Version, node.OSVersion,
		formatTime(time.Now().UTC()), node.ID)
	if err != nil {
		return fmt.Errorf("update node registration %q: %w", node.ID, err)
	}
	return requireChanged(result, node.ID)
}

// UpdateNodeOverlayIP changes a node's authoritative allocation.
func (s *Store) UpdateNodeOverlayIP(ctx context.Context, nodeID string, address netip.Addr) error {
	if !address.Is4() {
		return errors.New("overlay address must be IPv4")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET overlay_ip = ?, updated_at = ? WHERE node_id = ?
	`, address.String(), formatTime(time.Now().UTC()), nodeID)
	if err != nil {
		return fmt.Errorf("update node %q overlay address: %w", nodeID, err)
	}
	return requireChanged(result, nodeID)
}

// UpdateNodeName applies the Admin API's bounded display-name edit.
func (s *Store) UpdateNodeName(ctx context.Context, nodeID, name string) error {
	if name == "" || len(name) > 128 {
		return errors.New("node name must contain 1 to 128 bytes")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE nodes SET name = ?, updated_at = ? WHERE node_id = ?`,
		name, formatTime(time.Now().UTC()), nodeID)
	if err != nil {
		return fmt.Errorf("update node %q name: %w", nodeID, err)
	}
	return requireChanged(result, nodeID)
}

// ReplaceNodeOverlayIPs atomically migrates all Node allocations. Temporary
// values avoid UNIQUE collisions when old and new pools overlap.
func (s *Store) ReplaceNodeOverlayIPs(ctx context.Context, assignments map[string]netip.Addr) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceNodeOverlayIPs(ctx, tx, assignments); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceNodeOverlayIPsAndSetting commits the T18 Node allocation plan and
// authoritative network setting in one SQLite transaction. A Server crash can
// therefore observe either the complete old network or the complete new one,
// never new Node IPs paired with an old global Overlay configuration.
func (s *Store) ReplaceNodeOverlayIPsAndSetting(ctx context.Context, assignments map[string]netip.Addr, settingKey, settingValue string) error {
	if settingKey == "" {
		return errors.New("network setting key is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceNodeOverlayIPs(ctx, tx, assignments); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, settingKey, settingValue, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("set migration setting %q: %w", settingKey, err)
	}
	return tx.Commit()
}

type nodeOverlayTransaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func replaceNodeOverlayIPs(ctx context.Context, tx nodeOverlayTransaction, assignments map[string]netip.Addr) error {
	for nodeID, address := range assignments {
		if nodeID == "" || !address.Is4() {
			return errors.New("Overlay migration requires Node IDs and IPv4 addresses")
		}
		result, err := tx.ExecContext(ctx, `UPDATE nodes SET overlay_ip = ?, updated_at = ? WHERE node_id = ?`,
			"migrating:"+nodeID, formatTime(time.Now().UTC()), nodeID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
		}
	}
	for nodeID, address := range assignments {
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET overlay_ip = ?, updated_at = ? WHERE node_id = ?`,
			address.String(), formatTime(time.Now().UTC()), nodeID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteNode revokes a node and releases its unique address reservation.
func (s *Store) DeleteNode(ctx context.Context, nodeID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE node_id = ?`, nodeID)
	if err != nil {
		return fmt.Errorf("delete node %q: %w", nodeID, err)
	}
	return requireChanged(result, nodeID)
}

// UpdateNodeHeartbeat records liveness and current application metadata.
func (s *Store) UpdateNodeHeartbeat(ctx context.Context, nodeID string, status model.NodeStatus, at time.Time, version, osVersion string) error {
	if !status.Valid() {
		return fmt.Errorf("invalid node status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET status = ?, last_seen = ?, version = ?, os_version = ?, updated_at = ?
		WHERE node_id = ?
	`, status, formatTime(at), version, osVersion, formatTime(time.Now().UTC()), nodeID)
	if err != nil {
		return fmt.Errorf("update node %q heartbeat: %w", nodeID, err)
	}
	return requireChanged(result, nodeID)
}

// UpdateNodeStatus changes only the derived heartbeat status.
func (s *Store) UpdateNodeStatus(ctx context.Context, nodeID string, status model.NodeStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid node status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET status = ?, updated_at = ? WHERE node_id = ?
	`, status, formatTime(time.Now().UTC()), nodeID)
	if err != nil {
		return fmt.Errorf("update node %q status: %w", nodeID, err)
	}
	return requireChanged(result, nodeID)
}

const nodeSelect = `SELECT node_id, type, name, overlay_ip, wg_public_key,
	node_token_hash, status, version, os_version, last_seen, created_at, updated_at FROM nodes`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner rowScanner) (model.Node, error) {
	var (
		node      model.Node
		nodeType  string
		status    string
		overlayIP string
		lastSeen  sql.NullString
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(&node.ID, &nodeType, &node.Name, &overlayIP,
		&node.WGPublicKey, &node.NodeTokenHash, &status, &node.Version,
		&node.OSVersion, &lastSeen, &createdAt, &updatedAt); err != nil {
		return model.Node{}, err
	}
	parsedType, err := model.ParseNodeType(nodeType)
	if err != nil {
		return model.Node{}, err
	}
	node.Type = parsedType
	node.Status = model.NodeStatus(status)
	if !node.Status.Valid() {
		return model.Node{}, fmt.Errorf("invalid stored node status %q", status)
	}
	node.OverlayIP, err = netip.ParseAddr(overlayIP)
	if err != nil || !node.OverlayIP.Is4() {
		return model.Node{}, fmt.Errorf("invalid stored overlay address %q", overlayIP)
	}
	node.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return model.Node{}, fmt.Errorf("parse created_at: %w", err)
	}
	node.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return model.Node{}, fmt.Errorf("parse updated_at: %w", err)
	}
	if lastSeen.Valid {
		value, err := parseTime(lastSeen.String)
		if err != nil {
			return model.Node{}, fmt.Errorf("parse last_seen: %w", err)
		}
		node.LastSeen = &value
	}
	return node, nil
}

func validateNode(node model.Node) error {
	if node.ID == "" {
		return errors.New("node ID must not be empty")
	}
	if !node.Type.Valid() {
		return fmt.Errorf("invalid node type %q", node.Type)
	}
	if node.Name == "" {
		return errors.New("node name must not be empty")
	}
	if !node.OverlayIP.Is4() {
		return errors.New("overlay address must be IPv4")
	}
	if node.WGPublicKey == "" {
		return errors.New("WireGuard public key must not be empty")
	}
	if len(node.NodeTokenHash) == 0 {
		return errors.New("node token hash must not be empty")
	}
	if node.Status != "" && !node.Status.Valid() {
		return fmt.Errorf("invalid node status %q", node.Status)
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func requireChanged(result sql.Result, nodeID string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read changed rows: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	return nil
}
