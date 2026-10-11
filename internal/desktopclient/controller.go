// Package desktopclient owns saved desktop configuration and serializes Node
// restarts so changing settings never starts two network runtimes together.
package desktopclient

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"sync"
	"time"

	"remlink/internal/clientsettings"
	"remlink/internal/identity"
	"remlink/internal/localization"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/protocol"
	sessionruntime "remlink/internal/session"
)

type State struct {
	Configured       bool   `json:"configured"`
	Connecting       bool   `json:"connecting"`
	ServerConnected  bool   `json:"serverConnected"`
	ControlConnected bool   `json:"controlConnected"`
	ServerURL        string `json:"serverURL"`
	NodeName         string `json:"nodeName"`
	OverlayIP        string `json:"overlayIP"`
	ConnectionError  string `json:"connectionError"`
}

type Controller struct {
	mu         sync.RWMutex
	opMu       sync.Mutex
	callbackMu sync.Mutex
	options    nodeagent.Options
	settings   *clientsettings.Store
	identity   *identity.Store
	state      State
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	generation uint64
	run        func(context.Context, nodeagent.Options) error
	onState    func()
	onReset    func()
	onLog      func(string, string)
}

func New(options nodeagent.Options, settings *clientsettings.Store, identityStore *identity.Store,
	onState, onReset func(), onLog func(string, string)) *Controller {
	return &Controller{options: options, settings: settings, identity: identityStore, run: nodeagent.Run,
		onState: onState, onReset: onReset, onLog: onLog}
}

func (c *Controller) Snapshot() State { c.mu.RLock(); defer c.mu.RUnlock(); return c.state }
func (c *Controller) Settings() (clientsettings.Settings, error) {
	value, _, err := c.settings.Load()
	return value, err
}

func (c *Controller) Start(ctx context.Context) {
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
	if err := c.Reconnect(); err != nil {
		c.report("ERROR", err.Error())
	}
}

func (c *Controller) SaveSettings(value clientsettings.Settings) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	value = value.Normalize()
	if err := value.Validate(c.options.NodeType); err != nil {
		return err
	}
	if err := c.validateEnrollment(value); err != nil {
		return err
	}
	if err := c.settings.Save(value); err != nil {
		return err
	}
	c.report("INFO", "设置已保存，正在重新连接服务器")
	return c.connectLocked()
}

func (c *Controller) RenameNode(name string) error {
	value, err := c.Settings()
	if err != nil {
		return err
	}
	value.NodeName = name
	return c.SaveSettings(value)
}

func (c *Controller) Reconnect() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.connectLocked()
}

func (c *Controller) Close() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	_ = c.stopLocked()
}

func (c *Controller) validateEnrollment(value clientsettings.Settings) error {
	current, err := c.identity.Load()
	if errors.Is(err, os.ErrNotExist) {
		if value.JoinToken == "" {
			return errors.New("首次注册请填写 Join Token")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if (current.NodeToken == "" || current.ServerURL != value.ServerURL || current.RegisteredNodeName != value.NodeName) && value.JoinToken == "" {
		return errors.New("首次注册、修改服务器或设备名称时，请填写有效的 Join Token")
	}
	return nil
}

func (c *Controller) stopLocked() error {
	c.callbackMu.Lock()
	c.mu.Lock()
	c.generation++
	cancel, done := c.cancel, c.done
	c.cancel = nil
	c.mu.Unlock()
	c.callbackMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			return errors.New("旧连接仍在关闭，请稍后重试")
		}
	}
	c.mu.Lock()
	c.done = nil
	c.mu.Unlock()
	return nil
}

func (c *Controller) connectLocked() error {
	value, exists, err := c.settings.Load()
	if err != nil {
		return err
	}
	if exists {
		err = c.validateEnrollment(value)
	}
	if err != nil || !exists {
		c.mu.Lock()
		c.state = State{ServerURL: value.ServerURL, NodeName: value.NodeName}
		if err != nil {
			c.state.ConnectionError = err.Error()
		}
		c.mu.Unlock()
		c.emit()
		return nil
	}
	if err := c.stopLocked(); err != nil {
		return err
	}
	if c.onReset != nil {
		c.onReset()
	}
	c.mu.Lock()
	if c.ctx == nil || c.ctx.Err() != nil {
		c.mu.Unlock()
		return errors.New("客户端正在关闭，请重新启动")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.cancel = cancel
	done := make(chan struct{})
	c.done = done
	generation := c.generation
	c.state = State{Configured: true, Connecting: true, ServerURL: value.ServerURL, NodeName: value.NodeName}
	c.mu.Unlock()
	options := c.options
	options.ServerURL, options.NodeName, options.JoinToken = value.ServerURL, value.NodeName, value.JoinToken
	options.TCPFlowLimit, options.UDPFlowLimit = value.Netstack.TCPFlowLimit, value.Netstack.UDPFlowLimit
	options.UDPIdleTimeout = time.Duration(value.Netstack.UDPIdleSeconds) * time.Second
	live := func() bool { c.mu.RLock(); defer c.mu.RUnlock(); return c.generation == generation && ctx.Err() == nil }
	current := func(callback func()) {
		c.callbackMu.Lock()
		defer c.callbackMu.Unlock()
		if live() {
			callback()
		}
	}
	options.OnOverlayReady = func(address netip.Addr) {
		current(func() {
			c.mu.Lock()
			c.state.ServerConnected = true
			c.state.OverlayIP = address.String()
			c.mu.Unlock()
			if c.options.OnOverlayReady != nil {
				c.options.OnOverlayReady(address)
			}
			c.report("INFO", "已连接服务器，本机地址为 "+address.String())
		})
	}
	options.OnControlState = func(connected bool) {
		current(func() {
			c.mu.Lock()
			c.state.ControlConnected = connected
			c.state.Connecting = !connected
			c.mu.Unlock()
			if c.options.OnControlState != nil {
				c.options.OnControlState(connected)
			}
			if connected {
				c.report("INFO", "设备已上线")
			} else {
				c.report("WARN", "与服务器的连接已断开，正在重连")
			}
		})
	}
	options.OnNodeList = func(payload protocol.NodeListPayload) {
		current(func() {
			if c.options.OnNodeList != nil {
				c.options.OnNodeList(payload)
			}
		})
	}
	options.OnLatency = func(delay time.Duration) {
		current(func() {
			if c.options.OnLatency != nil {
				c.options.OnLatency(delay)
			}
		})
	}
	options.OnSession = func(status model.SessionStatus, id uint64, reason string) {
		current(func() {
			if c.options.OnSession != nil {
				c.options.OnSession(status, id, reason)
			}
		})
	}
	options.OnEngineerReady = func(engine *sessionruntime.EngineerRuntime) {
		current(func() {
			if c.options.OnEngineerReady != nil {
				c.options.OnEngineerReady(engine)
			}
		})
	}
	options.OnSiteReady = func() {
		current(func() {
			if c.options.OnSiteReady != nil {
				c.options.OnSiteReady()
			}
		})
	}
	options.OnRoute = func(id uint64, cidr netip.Prefix, result string) {
		current(func() {
			if c.options.OnRoute != nil {
				c.options.OnRoute(id, cidr, result)
			}
		})
	}
	c.emit()
	go func() {
		defer close(done)
		err := c.run(ctx, options)
		c.callbackMu.Lock()
		c.mu.Lock()
		if c.generation != generation {
			c.mu.Unlock()
			c.callbackMu.Unlock()
			return
		}
		c.generation++ // Ignore callbacks queued by the runtime that just ended.
		c.state.Connecting, c.state.ServerConnected, c.state.ControlConnected = false, false, false
		if err != nil && !errors.Is(err, context.Canceled) {
			c.state.ConnectionError = localization.ErrorMessage(err.Error())
		}
		c.mu.Unlock()
		if c.onReset != nil {
			c.onReset()
		}
		c.callbackMu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) {
			c.report("ERROR", localization.ErrorMessage(err.Error()))
		} else {
			c.emit()
		}
	}()
	return nil
}

func (c *Controller) emit() {
	if c.onState != nil {
		c.onState()
	}
}
func (c *Controller) report(level, message string) {
	if c.onLog != nil {
		c.onLog(level, message)
	}
	c.emit()
}
