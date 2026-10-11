//go:build windows

package desktopwindow

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
	"remlink/internal/platform/windows/tray"
)

type Window struct {
	mu      sync.RWMutex
	ctx     context.Context
	icon    *tray.Tray
	quit    bool
	uiReady bool
	asking  bool
}

func New() *Window { return &Window{} }

func (w *Window) Start(ctx context.Context, title, iconPath string) error {
	w.mu.Lock()
	w.ctx = ctx
	w.mu.Unlock()
	icon, err := tray.New(title, iconPath, func() { runtime.WindowShow(ctx); runtime.WindowUnminimise(ctx) }, w.Quit)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.icon = icon
	w.mu.Unlock()
	return nil
}

func (w *Window) BeforeClose(ctx context.Context) bool {
	w.mu.Lock()
	quit, ready, asking := w.quit, w.uiReady, w.asking
	if w.ctx == nil {
		w.ctx = ctx
	}
	if !ready {
		w.asking = true
	}
	w.mu.Unlock()
	if quit {
		return false
	}
	if !ready {
		if !asking {
			go w.fallbackClose()
		}
		return true
	}
	runtime.EventsEmit(ctx, "remlink:close-request")
	return true
}

func (w *Window) MarkUIReady() { w.mu.Lock(); w.uiReady = true; w.mu.Unlock() }

func (w *Window) fallbackClose() {
	defer func() { w.mu.Lock(); w.asking = false; w.mu.Unlock() }()
	message, _ := windows.UTF16PtrFromString("是否隐藏到系统托盘并继续后台运行？\n\n是 / Yes：隐藏到托盘\n否 / No：直接退出\n取消 / Cancel：返回窗口")
	title, _ := windows.UTF16PtrFromString("关闭 RemLink")
	choice, _ := windows.MessageBox(0, message, title, windows.MB_YESNOCANCEL|windows.MB_ICONQUESTION|windows.MB_SETFOREGROUND)
	if choice == 6 { // Win32 IDYES
		if err := w.HideToTray(); err != nil {
			text, _ := windows.UTF16PtrFromString(err.Error())
			_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONINFORMATION)
		}
	} else if choice == 7 { // Win32 IDNO
		w.Quit()
	}
}

func (w *Window) HideToTray() error {
	w.mu.RLock()
	icon, ctx := w.icon, w.ctx
	w.mu.RUnlock()
	if icon == nil {
		return errors.New("系统托盘未就绪，请选择直接退出或稍后重试")
	}
	runtime.WindowHide(ctx)
	return nil
}

func (w *Window) Quit() {
	w.mu.Lock()
	w.quit = true
	ctx := w.ctx
	w.mu.Unlock()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

func (w *Window) Close() {
	w.mu.Lock()
	icon := w.icon
	w.icon = nil
	w.mu.Unlock()
	if icon != nil {
		icon.Close()
	}
}
