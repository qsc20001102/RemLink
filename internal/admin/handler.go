package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"remlink/internal/database"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/protocol"
)

const maxAdminBody = 1 << 20

type JoinTokenRotator interface {
	Rotate(context.Context) (string, error)
}

type HandlerConfig struct {
	Store      *database.Store
	IPAM       IPAM
	Peers      PeerManager
	Control    ControlNetwork
	Sessions   SessionControl
	Network    *NetworkManager
	JoinTokens JoinTokenRotator
	AdminToken string
}

// Handler exposes only the exact /api/v1/admin surface from R11.
func Handler(config HandlerConfig) (http.Handler, error) {
	if config.Store == nil || config.IPAM == nil || config.Peers == nil || config.Control == nil ||
		config.Sessions == nil || config.Network == nil || config.JoinTokens == nil {
		return nil, errors.New("Admin handler dependencies are required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/nodes", func(writer http.ResponseWriter, request *http.Request) {
		nodes, err := config.Store.ListNodes(request.Context())
		if err != nil {
			writeResult(writer, nil, err)
			return
		}
		views := make([]nodeView, 0, len(nodes))
		for _, node := range nodes {
			handshake, handshakeErr := config.Peers.LastHandshake(request.Context(), node.WGPublicKey)
			if handshakeErr != nil {
				writeResult(writer, nil, handshakeErr)
				return
			}
			views = append(views, nodeView{Node: node, WGHandshake: handshake})
		}
		writeResult(writer, views, nil)
	})
	mux.HandleFunc("PATCH /api/v1/admin/nodes/{id}", func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			Name      *string `json:"name"`
			OverlayIP *string `json:"overlay_ip"`
		}
		if err := decodeAdminJSON(writer, request, &input); err != nil {
			writeAdminError(writer, http.StatusBadRequest, "INVALID_REQUEST", err)
			return
		}
		if input.Name == nil && input.OverlayIP == nil {
			writeAdminError(writer, http.StatusBadRequest, "INVALID_REQUEST", errors.New("name or overlay_ip is required"))
			return
		}
		var desiredName *string
		if input.Name != nil {
			name := strings.TrimSpace(*input.Name)
			if name == "" || len(name) > 128 {
				writeAdminError(writer, http.StatusBadRequest, "INVALID_NODE_NAME", errors.New("name must contain 1 to 128 bytes"))
				return
			}
			desiredName = &name
		}
		var desiredAddress netip.Addr
		if input.OverlayIP != nil {
			var parseErr error
			desiredAddress, parseErr = netip.ParseAddr(*input.OverlayIP)
			if parseErr != nil || !desiredAddress.Is4() {
				writeAdminError(writer, http.StatusBadRequest, "INVALID_OVERLAY_IP", errors.New("overlay_ip must be IPv4"))
				return
			}
		}
		nodeID := request.PathValue("id")
		before, err := config.Store.GetNode(request.Context(), nodeID)
		if err != nil {
			writeAdminError(writer, http.StatusNotFound, "NODE_NOT_FOUND", err)
			return
		}
		addressChanged := input.OverlayIP != nil && desiredAddress != before.OverlayIP
		if addressChanged {
			if err := config.Sessions.DisconnectNode(request.Context(), nodeID, "NODE_OVERLAY_IP_CHANGED"); err != nil {
				writeAdminError(writer, http.StatusConflict, "SESSION_DISCONNECT_FAILED", err)
				return
			}
			if err := config.IPAM.ChangeNodeAddress(request.Context(), nodeID, desiredAddress); err != nil {
				writeAdminError(writer, http.StatusConflict, "OVERLAY_IP_UNAVAILABLE", err)
				return
			}
			// The updated address is now visible through the public Bootstrap API.
			// Notify over the still-usable old Peer before replacing its /32;
			// changing the Peer first would cut the very Control path used by T17.
			current := config.Network.Current()
			_ = config.Control.Send(request.Context(), nodeID, protocol.ControlRebootstrapRequired,
				protocol.RebootstrapRequiredPayload{ConfigVersion: current.ConfigVersion, Reason: "NODE_OVERLAY_IP_CHANGED"})
			if err := config.Peers.EnsurePeer(request.Context(), before.WGPublicKey, desiredAddress); err != nil {
				_ = config.IPAM.ChangeNodeAddress(request.Context(), nodeID, before.OverlayIP)
				_ = config.Peers.EnsurePeer(request.Context(), before.WGPublicKey, before.OverlayIP)
				config.Control.ResetNodeConnection(nodeID, "Node Overlay IP update rolled back")
				writeAdminError(writer, http.StatusInternalServerError, "PEER_UPDATE_FAILED", err)
				return
			}
		}
		if desiredName != nil {
			if err := config.Store.UpdateNodeName(request.Context(), nodeID, *desiredName); err != nil {
				if addressChanged {
					_ = config.IPAM.ChangeNodeAddress(request.Context(), nodeID, before.OverlayIP)
					_ = config.Peers.EnsurePeer(request.Context(), before.WGPublicKey, before.OverlayIP)
					config.Control.ResetNodeConnection(nodeID, "Node update rolled back")
				}
				writeAdminError(writer, http.StatusBadRequest, "NODE_UPDATE_FAILED", err)
				return
			}
		}
		if addressChanged {
			config.Control.ResetNodeConnection(nodeID, "Node Overlay IP changed")
		}
		updated, err := config.Store.GetNode(request.Context(), nodeID)
		if err == nil {
			recordAdminEvent(request.Context(), config.Store, nodeID, 0, "节点配置已更新", map[string]any{"overlay_ip": updated.OverlayIP.String(), "name": updated.Name})
		}
		writeResult(writer, updated, err)
	})
	mux.HandleFunc("DELETE /api/v1/admin/nodes/{id}", func(writer http.ResponseWriter, request *http.Request) {
		nodeID := request.PathValue("id")
		node, err := config.Store.GetNode(request.Context(), nodeID)
		if err != nil {
			writeAdminError(writer, http.StatusNotFound, "NODE_NOT_FOUND", err)
			return
		}
		if err := config.Sessions.DisconnectNode(request.Context(), nodeID, "NODE_REVOKED"); err != nil {
			writeAdminError(writer, http.StatusConflict, "SESSION_DISCONNECT_FAILED", err)
			return
		}
		if err := config.Peers.RemovePeer(request.Context(), node.WGPublicKey); err != nil {
			writeAdminError(writer, http.StatusInternalServerError, "PEER_REVOKE_FAILED", err)
			return
		}
		if err := config.IPAM.ReleaseNode(request.Context(), nodeID); err != nil {
			_ = config.Peers.EnsurePeer(request.Context(), node.WGPublicKey, node.OverlayIP)
			writeAdminError(writer, http.StatusInternalServerError, "NODE_DELETE_FAILED", err)
			return
		}
		config.Control.ResetNodeConnection(nodeID, "Node revoked")
		recordAdminEvent(request.Context(), config.Store, nodeID, 0, "节点已撤销", nil)
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/admin/sessions", func(writer http.ResponseWriter, request *http.Request) {
		sessions, err := config.Store.ListSessions(request.Context())
		writeResult(writer, sessions, err)
	})
	mux.HandleFunc("POST /api/v1/admin/sessions/{id}/disconnect", func(writer http.ResponseWriter, request *http.Request) {
		id, err := strconv.ParseUint(request.PathValue("id"), 10, 64)
		if err != nil || id == 0 {
			writeAdminError(writer, http.StatusBadRequest, "INVALID_SESSION_ID", errors.New("SessionID must be uint64"))
			return
		}
		if err := config.Sessions.Disconnect(request.Context(), id, "ADMIN_DISCONNECT"); err != nil {
			writeAdminError(writer, http.StatusConflict, "SESSION_DISCONNECT_FAILED", err)
			return
		}
		recordAdminEvent(request.Context(), config.Store, "", id, "管理员已强制断开会话", nil)
		writeAdminJSON(writer, http.StatusOK, map[string]any{"session_id": id, "status": model.SessionClosed})
	})
	mux.HandleFunc("GET /api/v1/admin/network", func(writer http.ResponseWriter, _ *http.Request) {
		writeAdminJSON(writer, http.StatusOK, config.Network.View())
	})
	mux.HandleFunc("PUT /api/v1/admin/network", func(writer http.ResponseWriter, request *http.Request) {
		var input NetworkUpdate
		if err := decodeAdminJSON(writer, request, &input); err != nil {
			writeAdminError(writer, http.StatusBadRequest, "INVALID_REQUEST", err)
			return
		}
		current := config.Network.Current()
		updated := current
		var err error
		if !networkInputMatches(input, current) {
			updated, err = config.Network.Update(request.Context(), input)
			if err != nil {
				writeAdminError(writer, http.StatusConflict, "NETWORK_UPDATE_FAILED", err)
				return
			}
		}
		response := struct {
			Network
			JoinToken string `json:"join_token,omitempty"`
		}{Network: updated}
		if input.RotateJoinToken {
			response.JoinToken, err = config.JoinTokens.Rotate(request.Context())
			if err != nil {
				writeAdminError(writer, http.StatusInternalServerError, "JOIN_TOKEN_ROTATE_FAILED", err)
				return
			}
		}
		recordAdminEvent(request.Context(), config.Store, "", 0, "网络配置已更新", map[string]any{"config_version": updated.ConfigVersion, "join_token_rotated": input.RotateJoinToken})
		writeAdminJSON(writer, http.StatusOK, response)
	})
	mux.HandleFunc("GET /api/v1/admin/logs", func(writer http.ResponseWriter, request *http.Request) {
		filter := model.EventLogFilter{
			Level: request.URL.Query().Get("level"), Module: request.URL.Query().Get("module"),
			NodeID: request.URL.Query().Get("node_id"),
		}
		if raw := request.URL.Query().Get("session_id"); raw != "" {
			var parseErr error
			filter.SessionID, parseErr = strconv.ParseUint(raw, 10, 64)
			if parseErr != nil || filter.SessionID == 0 {
				writeAdminError(writer, http.StatusBadRequest, "INVALID_SESSION_ID", errors.New("session_id must be uint64"))
				return
			}
		}
		if raw := request.URL.Query().Get("limit"); raw != "" {
			var parseErr error
			filter.Limit, parseErr = strconv.Atoi(raw)
			if parseErr != nil || filter.Limit < 1 || filter.Limit > 1000 {
				writeAdminError(writer, http.StatusBadRequest, "INVALID_LIMIT", errors.New("limit must be between 1 and 1000"))
				return
			}
		}
		for name, destination := range map[string]*time.Time{"from": &filter.From, "to": &filter.To} {
			if raw := request.URL.Query().Get(name); raw != "" {
				parsed, parseErr := time.Parse(time.RFC3339, raw)
				if parseErr != nil {
					writeAdminError(writer, http.StatusBadRequest, "INVALID_TIME", fmt.Errorf("%s must be RFC3339", name))
					return
				}
				*destination = parsed
			}
		}
		if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
			writeAdminError(writer, http.StatusBadRequest, "INVALID_TIME_RANGE", errors.New("from must not be after to"))
			return
		}
		events, err := config.Store.ListEvents(request.Context(), filter)
		writeResult(writer, events, err)
	})
	return adminSecurity(config.AdminToken, mux), nil
}

type nodeView struct {
	model.Node
	WGHandshake *time.Time `json:"wg_handshake,omitempty"`
}

func networkInputMatches(input NetworkUpdate, current Network) bool {
	return input.OverlayCIDR == current.OverlayCIDR && input.ServerOverlayIP == current.ServerOverlayIP &&
		input.WireGuardPort == current.WireGuardPort && input.SessionUDPPort == current.SessionUDPPort && input.MTU == current.MTU
}

func recordAdminEvent(ctx context.Context, store *database.Store, nodeID string, sessionID uint64, message string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	raw, _ := json.Marshal(fields)
	_ = store.AppendEvent(ctx, model.EventLog{
		Level: "INFO", Module: string(logging.ModuleCore), NodeID: nodeID, SessionID: sessionID,
		Message: message, FieldsJSON: raw,
	})
}

func adminSecurity(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		if token != "" {
			provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
			if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				writeAdminError(writer, http.StatusUnauthorized, "ADMIN_AUTH_FAILED", errors.New("valid Bearer Admin Token required"))
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func decodeAdminJSON(writer http.ResponseWriter, request *http.Request, destination any) error {
	if contentType := request.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxAdminBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeResult(writer http.ResponseWriter, value any, err error) {
	if err != nil {
		writeAdminError(writer, http.StatusInternalServerError, "ADMIN_OPERATION_FAILED", err)
		return
	}
	writeAdminJSON(writer, http.StatusOK, value)
}

func writeAdminError(writer http.ResponseWriter, status int, code string, err error) {
	writeAdminJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
}

func writeAdminJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
