//go:build windows

package tray

import (
	"testing"
	"time"
)

func TestNativeTrayCreatesReceivesActivationAndCloses(t *testing.T) {
	activated := make(chan struct{}, 1)
	icon, err := New("RemLink tray verification", "", func() { activated <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer icon.Close()
	window := icon.window.Load()
	if window == 0 {
		t.Fatal("tray has no callback window")
	}
	postMessage.Call(window, trayMessage, 0, 0x0202)
	select {
	case <-activated:
	case <-time.After(time.Second):
		t.Fatal("tray activation did not restore the application callback")
	}
	icon.Close()
	if icon.window.Load() != 0 {
		t.Fatal("tray window survived cleanup")
	}
}
