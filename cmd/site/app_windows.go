//go:build windows

package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"remlink/internal/clientsettings"
	"remlink/internal/desktopclient"
	"remlink/internal/desktopwindow"
	"remlink/internal/identity"
	"remlink/internal/localization"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
)

type SiteLog struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}
type SiteSession struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type SiteState struct {
	desktopclient.State
	Version      string        `json:"version"`
	GatewayReady bool          `json:"gatewayReady"`
	Sessions     []SiteSession `json:"sessions"`
	Logs         []SiteLog     `json:"logs"`
}
type SiteApp struct {
	mu           sync.RWMutex
	ctx          context.Context
	controller   *desktopclient.Controller
	window       *desktopwindow.Window
	version      string
	gatewayReady bool
	sessions     map[string]SiteSession
	logs         []SiteLog
}

func NewSiteApp(options nodeagent.Options, settings *clientsettings.Store, identityStore *identity.Store) *SiteApp {
	a := &SiteApp{version: options.Version, sessions: make(map[string]SiteSession), logs: []SiteLog{}, window: desktopwindow.New()}
	options.OnSiteReady = func() {
		a.mu.Lock()
		a.gatewayReady = true
		a.mu.Unlock()
		a.appendLog("INFO", "现场网络已就绪，可以接收工程师连接")
		a.emit()
	}
	options.OnSession = func(status model.SessionStatus, id uint64, reason string) {
		key := strconv.FormatUint(id, 10)
		a.mu.Lock()
		if status == model.SessionClosed || status == model.SessionFailed {
			delete(a.sessions, key)
		} else {
			a.sessions[key] = SiteSession{ID: key, Status: string(status), Reason: reason}
		}
		a.mu.Unlock()
		message := fmt.Sprintf("工程师连接 %s：%s", key, localization.SessionStatus(string(status)))
		if reason != "" {
			message += "；" + localization.Reason(reason)
		}
		a.appendLog("INFO", message)
		a.emit()
	}
	a.controller = desktopclient.New(options, settings, identityStore, a.emit, func() {
		a.mu.Lock()
		a.gatewayReady = false
		a.sessions = make(map[string]SiteSession)
		a.mu.Unlock()
	}, a.appendLog)
	return a
}

func (a *SiteApp) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	a.controller.Start(ctx)
}
func (a *SiteApp) Shutdown(context.Context)                      { a.controller.Close(); a.window.Close() }
func (a *SiteApp) GetSettings() (clientsettings.Settings, error) { return a.controller.Settings() }
func (a *SiteApp) SaveSettings(value clientsettings.Settings) error {
	return a.controller.SaveSettings(value)
}
func (a *SiteApp) RenameNode(name string) error { return a.controller.RenameNode(name) }
func (a *SiteApp) ReconnectServer() error       { return a.controller.Reconnect() }
func (a *SiteApp) HideToTray() error            { return a.window.HideToTray() }
func (a *SiteApp) QuitApp()                     { a.window.Quit() }
func (a *SiteApp) GetState() SiteState {
	a.window.MarkUIReady()
	a.mu.RLock()
	state := SiteState{Version: a.version, GatewayReady: a.gatewayReady, Sessions: []SiteSession{}, Logs: append([]SiteLog{}, a.logs...)}
	for _, session := range a.sessions {
		state.Sessions = append(state.Sessions, session)
	}
	a.mu.RUnlock()
	sort.Slice(state.Sessions, func(i, j int) bool { return state.Sessions[i].ID < state.Sessions[j].ID })
	state.State = a.controller.Snapshot()
	return state
}
func (a *SiteApp) appendLog(level, message string) {
	a.mu.Lock()
	a.logs = append(a.logs, SiteLog{Time: time.Now().UTC(), Level: level, Message: message})
	if len(a.logs) > 200 {
		a.logs = append([]SiteLog{}, a.logs[len(a.logs)-200:]...)
	}
	a.mu.Unlock()
}
func (a *SiteApp) emit() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "remlink:state")
	}
}
