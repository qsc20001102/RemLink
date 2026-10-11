// Package clientsettings persists desktop settings independently of a software
// package. Registration tokens use the same Windows protector as Node secrets.
package clientsettings

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/model"
)

type Settings struct {
	ServerURL string                    `json:"serverURL"`
	NodeName  string                    `json:"nodeName"`
	JoinToken string                    `json:"joinToken"`
	Netstack  config.SiteNetstackConfig `json:"netstack"`
}

func Defaults(role model.NodeType) Settings {
	return Settings{Netstack: config.SiteNetstackConfig{
		TCPFlowLimit: config.DefaultSiteTCPFlowLimit, UDPFlowLimit: config.DefaultSiteUDPFlowLimit,
		UDPIdleSeconds: config.DefaultSiteUDPIdleSeconds,
	}}
}

func (s Settings) Normalize() Settings {
	s.ServerURL = strings.TrimRight(strings.TrimSpace(s.ServerURL), "/")
	s.NodeName = strings.TrimSpace(s.NodeName)
	s.JoinToken = strings.TrimSpace(s.JoinToken)
	return s
}

func (s Settings) Validate(role model.NodeType) error {
	if !role.Valid() {
		return errors.New("无效的客户端角色")
	}
	if err := config.ValidateServerURL(s.ServerURL); err != nil {
		return errors.New("请填写有效的 Server 公网地址，例如 https://remlink.example.com，不包含额外路径")
	}
	if s.NodeName == "" || len(s.NodeName) > 128 {
		return errors.New("请填写设备名称，长度不能超过 128 字节")
	}
	if role == model.NodeTypeSite && (s.Netstack.TCPFlowLimit < 1 || s.Netstack.UDPFlowLimit < 1 || s.Netstack.UDPIdleSeconds < 1 || s.Netstack.UDPIdleSeconds > 86400) {
		return errors.New("现场端容量与 UDP 超时设置无效")
	}
	return nil
}

type diskSettings struct {
	Version            int                       `json:"version"`
	Role               model.NodeType            `json:"role"`
	ServerURL          string                    `json:"server_url"`
	NodeName           string                    `json:"node_name"`
	ProtectedJoinToken string                    `json:"protected_join_token,omitempty"`
	Netstack           config.SiteNetstackConfig `json:"netstack"`
}

type Store struct {
	mu        sync.Mutex
	path      string
	role      model.NodeType
	protector identity.Protector
}

func NewStore(path string, role model.NodeType, protector identity.Protector) (*Store, error) {
	if path == "" || !role.Valid() || protector == nil {
		return nil, errors.New("settings require a path, Node role, and protector")
	}
	return &Store{path: path, role: role, protector: protector}, nil
}

func (s *Store) Load() (Settings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(s.role), false, nil
	}
	if err != nil {
		return Settings{}, false, err
	}
	var disk diskSettings
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil {
		return Settings{}, false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Settings{}, false, errors.New("客户端设置文件格式无效")
	}
	if disk.Version != 1 || disk.Role != s.role {
		return Settings{}, false, errors.New("客户端设置版本或角色不匹配")
	}
	value := Settings{ServerURL: disk.ServerURL, NodeName: disk.NodeName, Netstack: disk.Netstack}
	if disk.ProtectedJoinToken != "" {
		cipher, err := base64.StdEncoding.DecodeString(disk.ProtectedJoinToken)
		if err != nil {
			return Settings{}, false, err
		}
		plain, err := s.protector.Unprotect(cipher)
		if err != nil {
			return Settings{}, false, fmt.Errorf("读取本机 Join Token 失败：%w", err)
		}
		value.JoinToken = string(plain)
		clear(plain)
	}
	value = value.Normalize()
	if err := value.Validate(s.role); err != nil {
		return Settings{}, false, err
	}
	return value, true, nil
}

func (s *Store) Save(value Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = value.Normalize()
	if err := value.Validate(s.role); err != nil {
		return err
	}
	disk := diskSettings{Version: 1, Role: s.role, ServerURL: value.ServerURL, NodeName: value.NodeName, Netstack: value.Netstack}
	if value.JoinToken != "" {
		plain := []byte(value.JoinToken)
		cipher, err := s.protector.Protect(plain)
		clear(plain)
		if err != nil {
			return err
		}
		disk.ProtectedJoinToken = base64.StdEncoding.EncodeToString(cipher)
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, err = file.Write(append(raw, '\n'))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	return replace(file.Name(), s.path)
}
