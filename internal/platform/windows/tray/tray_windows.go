//go:build windows

// Package tray owns a Windows notification icon and its native message loop.
package tray

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	unregisterClass  = user32.NewProc("UnregisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	registerMessage  = user32.NewProc("RegisterWindowMessageW")
	loadImage        = user32.NewProc("LoadImageW")
	loadIcon         = user32.NewProc("LoadIconW")
	destroyIcon      = user32.NewProc("DestroyIcon")
	createPopup      = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	destroyMenu      = user32.NewProc("DestroyMenu")
	trackPopup       = user32.NewProc("TrackPopupMenu")
	getCursorPos     = user32.NewProc("GetCursorPos")
	setForeground    = user32.NewProc("SetForegroundWindow")
	notifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	getModule        = kernel32.NewProc("GetModuleHandleW")
	classSequence    atomic.Uint64
)

const trayMessage = 0x8000 + 73

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type notification struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}
type Tray struct {
	window     atomic.Uintptr
	closed     chan struct{}
	closeOnce  sync.Once
	open, quit func()
}

func New(title, iconPath string, open, quit func()) (*Tray, error) {
	t := &Tray{closed: make(chan struct{}), open: open, quit: quit}
	ready := make(chan error, 1)
	go t.loop(title, iconPath, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Tray) Close() {
	t.closeOnce.Do(func() {
		if window := t.window.Load(); window != 0 {
			postMessage.Call(window, 0x0010, 0, 0)
		}
	})
	<-t.closed
}

func (t *Tray) loop(title, iconPath string, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.closed)
	instance, _, _ := getModule.Call(0)
	className, _ := windows.UTF16PtrFromString(fmt.Sprintf("RemLinkTray-%d-%d", windows.GetCurrentProcessId(), classSequence.Add(1)))
	var icon uintptr
	ownedIcon := false
	if iconPath != "" {
		path, err := windows.UTF16PtrFromString(iconPath)
		if err == nil {
			icon, _, _ = loadImage.Call(0, uintptr(unsafe.Pointer(path)), 1, 0, 0, 0x10)
			ownedIcon = icon != 0
		}
	}
	if icon == 0 {
		icon, _, _ = loadIcon.Call(instance, 1)
	}
	if icon == 0 {
		icon, _, _ = loadIcon.Call(0, 32512)
	}
	if ownedIcon {
		defer destroyIcon.Call(icon)
	}
	var data notification
	add := func() bool {
		result, _, _ := notifyIcon.Call(0, uintptr(unsafe.Pointer(&data)))
		if result != 0 {
			data.Version = 4
			notifyIcon.Call(4, uintptr(unsafe.Pointer(&data)))
		}
		return result != 0
	}
	taskbarName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbarMessage, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(taskbarName)))
	callback := windows.NewCallback(func(window uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch {
		case msg == trayMessage:
			switch uint32(lParam) & 0xffff {
			case 0x0400, 0x0401, 0x0202, 0x0203:
				if t.open != nil {
					go t.open()
				}
			case 0x007b, 0x0205:
				t.menu(window, &data)
			}
			return 0
		case taskbarMessage != 0 && uintptr(msg) == taskbarMessage:
			add()
			return 0
		case msg == 0x0010:
			notifyIcon.Call(2, uintptr(unsafe.Pointer(&data)))
			destroyWindow.Call(window)
			return 0
		case msg == 0x0002:
			postQuitMessage.Call(0)
			return 0
		}
		result, _, _ := defWindowProc.Call(window, uintptr(msg), wParam, lParam)
		return result
	})
	class := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), Procedure: callback, Instance: instance, ClassName: className}
	atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		ready <- fmt.Errorf("注册托盘窗口失败：%w", err)
		return
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(className)), instance)
	window, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if window == 0 {
		ready <- fmt.Errorf("创建托盘窗口失败：%w", err)
		return
	}
	defer destroyWindow.Call(window)
	t.window.Store(window)
	defer t.window.Store(0)
	data = notification{Size: uint32(unsafe.Sizeof(notification{})), Window: window, ID: 1, Flags: 1 | 2 | 4 | 0x80, Callback: trayMessage, Icon: icon}
	tip, _ := windows.UTF16FromString(title)
	copy(data.Tip[:127], tip)
	if !add() {
		ready <- fmt.Errorf("系统托盘当前不可用")
		return
	}
	ready <- nil
	var msg message
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	notifyIcon.Call(2, uintptr(unsafe.Pointer(&data)))
}

func (t *Tray) menu(window uintptr, data *notification) {
	menu, _, _ := createPopup.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	show, _ := windows.UTF16PtrFromString("显示主窗口")
	exit, _ := windows.UTF16PtrFromString("退出 RemLink")
	appendMenu.Call(menu, 0, 1, uintptr(unsafe.Pointer(show)))
	appendMenu.Call(menu, 0x800, 0, 0)
	appendMenu.Call(menu, 0, 2, uintptr(unsafe.Pointer(exit)))
	var cursor point
	getCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	setForeground.Call(window)
	command, _, _ := trackPopup.Call(menu, 0x0100|0x0080|0x0002, uintptr(cursor.X), uintptr(cursor.Y), 0, window, 0)
	postMessage.Call(window, 0, 0, 0)
	notifyIcon.Call(3, uintptr(unsafe.Pointer(data)))
	if command == 1 && t.open != nil {
		go t.open()
	}
	if command == 2 && t.quit != nil {
		go t.quit()
	}
}
