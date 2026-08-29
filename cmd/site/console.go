package main

import (
	"fmt"
	"io"
	"net/netip"
	"sync"
	"time"

	"remlink/internal/localization"
	"remlink/internal/model"
)

type siteConsole struct {
	mu sync.Mutex
	w  io.Writer
}

func newSiteConsole(w io.Writer) *siteConsole { return &siteConsole{w: w} }

func (c *siteConsole) line(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.w, "%s ", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(c.w, format, args...)
	fmt.Fprintln(c.w)
}

func (c *siteConsole) Header(server, node, version string) {
	c.line("RemLink Site %s", version)
	c.line("服务器=%s 节点=%s", server, node)
	c.line("OverlayIP=等待分配 WireGuard=正在连接 Control=正在连接 远程网段=正在初始化 子网网关=正在初始化")
}

func (c *siteConsole) OverlayReady(address netip.Addr) {
	c.line("OverlayIP=%s WireGuard=已连接", address)
}

func (c *siteConsole) ControlState(online bool) {
	state := "正在重连"
	if online {
		state = "已连接"
	}
	c.line("Control=%s", state)
}

func (c *siteConsole) SiteReady() {
	c.line("远程网段=就绪 子网网关=gVisor netstack/就绪")
}

func (c *siteConsole) Session(status model.SessionStatus, sessionID uint64, reason string) {
	if reason == "" {
		reason = "无"
	} else {
		reason = localization.Reason(reason)
	}
	c.line("[会话/SESSION] ID=%d 状态=%s 原因=%s", sessionID, localization.SessionStatus(string(status)), reason)
}

func (c *siteConsole) Route(sessionID uint64, prefix netip.Prefix, result string) {
	c.line("[路由/ROUTE] 会话=%d 网段=%s 结果=%s", sessionID, prefix, localization.RouteResult(result))
}
