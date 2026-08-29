//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"remlink/internal/localization"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/protocol"
	sessionruntime "remlink/internal/session"
	"remlink/internal/siteprofile"
)

type EngineerApp struct {
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	options  nodeagent.Options
	engine   *sessionruntime.EngineerRuntime
	profiles *siteprofile.Store
	state    EngineerState
}

type EngineerState struct {
	ServerConnected  bool                   `json:"serverConnected"`
	ControlConnected bool                   `json:"controlConnected"`
	ServerURL        string                 `json:"serverURL"`
	Version          string                 `json:"version"`
	OverlayIP        string                 `json:"overlayIP"`
	Sites            []protocol.SiteSummary `json:"sites"`
	SiteCIDRs        map[string][]string    `json:"siteCIDRs"`
	Session          EngineerSessionState   `json:"session"`
	Logs             []EngineerLog          `json:"logs"`
}

type EngineerSessionState struct {
	ID              string     `json:"id"`
	SiteName        string     `json:"siteName"`
	CIDRs           []string   `json:"cidrs"`
	Status          string     `json:"status"`
	UploadBytes     uint64     `json:"uploadBytes"`
	DownloadBytes   uint64     `json:"downloadBytes"`
	UploadPackets   uint64     `json:"uploadPackets"`
	DownloadPackets uint64     `json:"downloadPackets"`
	LatencyMS       int        `json:"latencyMS"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

type EngineerLog struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

func NewEngineerApp(options nodeagent.Options, profiles *siteprofile.Store) *EngineerApp {
	return &EngineerApp{options: options, profiles: profiles, state: EngineerState{
		ServerURL: options.ServerURL, Version: options.Version,
		Sites: []protocol.SiteSummary{}, SiteCIDRs: map[string][]string{}, Logs: []EngineerLog{},
		Session: EngineerSessionState{CIDRs: []string{}, Status: "IDLE"},
	}}
}

func (a *EngineerApp) CheckCIDRs(cidrs []string) error {
	a.mu.RLock()
	engine := a.engine
	a.mu.RUnlock()
	if engine == nil {
		return errors.New("Engineer 网络运行时尚未就绪")
	}
	return engine.PreflightCIDRs(cidrs)
}

func (a *EngineerApp) Startup(wailsContext context.Context) {
	a.mu.Lock()
	a.ctx, a.cancel = context.WithCancel(wailsContext)
	ctx := a.ctx
	a.mu.Unlock()
	if a.profiles != nil {
		profiles, err := a.profiles.Load()
		if err != nil {
			a.appendLog("ERROR", "读取现场网段配置失败："+err.Error())
		} else {
			a.mu.Lock()
			for siteID, profile := range profiles {
				a.state.SiteCIDRs[siteID] = append([]string(nil), profile.CIDRs...)
			}
			a.mu.Unlock()
		}
	}
	options := a.options
	options.OnOverlayReady = func(address netip.Addr) {
		a.mu.Lock()
		a.state.ServerConnected = true
		a.state.OverlayIP = address.String()
		a.mu.Unlock()
		a.appendLog("INFO", "Overlay 已就绪："+address.String())
		a.emit()
	}
	options.OnControlState = func(connected bool) {
		a.mu.Lock()
		a.state.ControlConnected = connected
		a.mu.Unlock()
		if connected {
			a.appendLog("INFO", "Control WebSocket 已连接")
		} else {
			a.appendLog("WARN", "Control WebSocket 已断开，正在重连")
		}
		a.emit()
	}
	options.OnLatency = func(delay time.Duration) {
		milliseconds := int(delay.Round(time.Millisecond) / time.Millisecond)
		if milliseconds < 1 {
			milliseconds = 1
		}
		a.mu.Lock()
		a.state.Session.LatencyMS = milliseconds
		a.mu.Unlock()
		a.emit()
	}
	options.OnNodeList = func(payload protocol.NodeListPayload) {
		a.mu.Lock()
		a.state.Sites = append([]protocol.SiteSummary{}, payload.Sites...)
		a.mu.Unlock()
		a.emit()
	}
	options.OnSession = a.onSession
	options.OnEngineerReady = func(engine *sessionruntime.EngineerRuntime) {
		a.mu.Lock()
		a.engine = engine
		a.mu.Unlock()
		a.emit()
	}
	go func() {
		if err := nodeagent.Run(ctx, options); err != nil && !errors.Is(err, context.Canceled) {
			a.appendLog("ERROR", localization.ErrorMessage(err.Error()))
			a.mu.Lock()
			a.state.ServerConnected = false
			a.state.ControlConnected = false
			a.mu.Unlock()
			a.emit()
		}
	}()
}

func (a *EngineerApp) Shutdown(context.Context) {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
}

func (a *EngineerApp) GetState() EngineerState {
	a.mu.RLock()
	state := a.state
	// Wails serialises nil slices as JSON null. The frontend contract requires
	// arrays even when they are empty, otherwise the first Vue render fails.
	state.Sites = append([]protocol.SiteSummary{}, a.state.Sites...)
	state.SiteCIDRs = make(map[string][]string, len(a.state.SiteCIDRs))
	for siteID, cidrs := range a.state.SiteCIDRs {
		state.SiteCIDRs[siteID] = append([]string(nil), cidrs...)
	}
	state.Logs = append([]EngineerLog{}, a.state.Logs...)
	state.Session.CIDRs = append([]string{}, a.state.Session.CIDRs...)
	engine := a.engine
	a.mu.RUnlock()
	if engine != nil {
		snapshot := engine.Snapshot()
		if snapshot.ID != 0 {
			state.Session.ID = strconv.FormatUint(snapshot.ID, 10)
			state.Session.Status = string(snapshot.Status)
			state.Session.UploadBytes = snapshot.Counters.UploadBytes
			state.Session.DownloadBytes = snapshot.Counters.DownloadBytes
			state.Session.UploadPackets = snapshot.Counters.UploadPackets
			state.Session.DownloadPackets = snapshot.Counters.DownloadPackets
		}
	}
	return state
}

// SaveSiteCIDRs persists one Site's non-secret Remote CIDR profile beside the
// Engineer executable. An empty list clears only that Site's profile.
func (a *EngineerApp) SaveSiteCIDRs(siteNodeID string, cidrs []string) error {
	a.mu.RLock()
	engine := a.engine
	sites := append([]protocol.SiteSummary(nil), a.state.Sites...)
	profiles := a.profiles
	a.mu.RUnlock()
	if profiles == nil {
		return errors.New("现场网段配置存储尚未就绪")
	}
	siteName := ""
	for _, site := range sites {
		if site.NodeID == siteNodeID {
			siteName = site.Name
			break
		}
	}
	if siteName == "" {
		return errors.New("所选 Site 不存在")
	}
	if len(cidrs) > 0 {
		if engine == nil {
			return errors.New("Engineer 网络运行时尚未就绪")
		}
		if err := engine.PreflightCIDRs(cidrs); err != nil {
			return err
		}
	}
	if err := profiles.Save(siteNodeID, siteName, cidrs); err != nil {
		return err
	}
	a.mu.Lock()
	if len(cidrs) == 0 {
		delete(a.state.SiteCIDRs, siteNodeID)
	} else {
		a.state.SiteCIDRs[siteNodeID] = append([]string(nil), cidrs...)
	}
	a.mu.Unlock()
	a.appendLog("INFO", fmt.Sprintf("已保存 %s 的远程网段配置（%d 个）", siteName, len(cidrs)))
	a.emit()
	return nil
}

func (a *EngineerApp) CreateSession(siteNodeID string, cidrs []string) (string, error) {
	a.mu.RLock()
	engine, ctx := a.engine, a.ctx
	sites := append([]protocol.SiteSummary(nil), a.state.Sites...)
	a.mu.RUnlock()
	if engine == nil || ctx == nil {
		return "", errors.New("Engineer 网络运行时尚未就绪")
	}
	if len(cidrs) == 0 {
		return "", errors.New("至少输入一个 Remote CIDR")
	}
	siteName := ""
	for _, site := range sites {
		if site.NodeID == siteNodeID {
			if !site.Online {
				return "", errors.New("所选 Site 当前离线")
			}
			if !site.RemoteSubnetCapability {
				return "", errors.New("所选 Site 不支持 Remote Subnet")
			}
			siteName = site.Name
			break
		}
	}
	if siteName == "" {
		return "", errors.New("所选 Site 不存在")
	}
	if err := a.SaveSiteCIDRs(siteNodeID, cidrs); err != nil {
		return "", err
	}
	a.mu.Lock()
	previousSession := a.state.Session
	now := time.Now().UTC()
	a.state.Session = EngineerSessionState{
		SiteName: siteName, CIDRs: append([]string(nil), cidrs...), Status: "IDLE",
		LatencyMS: previousSession.LatencyMS, StartedAt: &now,
	}
	a.mu.Unlock()
	requestID, err := engine.CreateSession(ctx, siteNodeID, cidrs)
	if err != nil {
		a.mu.Lock()
		a.state.Session = previousSession
		a.mu.Unlock()
		a.appendLog("ERROR", localization.ErrorMessage(err.Error()))
		a.emit()
		return "", err
	}
	a.appendLog("INFO", fmt.Sprintf("正在连接 %s：%v", siteName, cidrs))
	a.emit()
	return requestID, nil
}

func (a *EngineerApp) DisconnectSession() error {
	a.mu.RLock()
	engine, ctx := a.engine, a.ctx
	a.mu.RUnlock()
	if engine == nil || ctx == nil {
		return errors.New("Engineer 网络运行时尚未就绪")
	}
	return engine.Disconnect(ctx, "ENGINEER_OPERATOR")
}

func (a *EngineerApp) onSession(status model.SessionStatus, sessionID uint64, reason string) {
	a.mu.Lock()
	a.state.Session.Status = string(status)
	if sessionID != 0 {
		a.state.Session.ID = strconv.FormatUint(sessionID, 10)
	}
	a.state.Session.Reason = reason
	if status == model.SessionClosed || status == model.SessionFailed {
		a.state.Session.StartedAt = nil
	}
	a.mu.Unlock()
	message := fmt.Sprintf("会话 %d → %s", sessionID, localization.SessionStatus(string(status)))
	if reason != "" {
		message += "；原因：" + localization.Reason(reason)
	}
	a.appendLog("INFO", message)
	a.emit()
}

func (a *EngineerApp) appendLog(level, message string) {
	a.mu.Lock()
	a.state.Logs = append(a.state.Logs, EngineerLog{Time: time.Now().UTC(), Level: level, Message: message})
	if len(a.state.Logs) > 200 {
		a.state.Logs = append([]EngineerLog(nil), a.state.Logs[len(a.state.Logs)-200:]...)
	}
	a.mu.Unlock()
}

func (a *EngineerApp) emit() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "remlink:state")
	}
}
