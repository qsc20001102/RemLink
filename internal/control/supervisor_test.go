package control

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func TestSupervisorRebindMovesListenerAndPreservesFailedRebind(t *testing.T) {
	oldAddress := netip.MustParseAddr("127.0.0.1")
	newAddress := netip.MustParseAddr("127.0.0.2")
	port := availablePort(t, oldAddress)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(writer, "ok") })
	supervisor, err := NewSupervisor(handler, port)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := supervisor.Start(ctx, oldAddress); err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	assertHTTPBody(t, client, oldAddress, port, "ok")

	occupied, err := net.Listen("tcp4", net.JoinHostPort(newAddress.String(), strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Rebind(newAddress); err == nil {
		occupied.Close()
		t.Fatal("Rebind succeeded while the target address was occupied")
	}
	assertHTTPBody(t, client, oldAddress, port, "ok")
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}

	if err := supervisor.Rebind(newAddress); err != nil {
		t.Fatal(err)
	}
	assertHTTPBody(t, client, newAddress, port, "ok")
}

func availablePort(t *testing.T, address netip.Addr) int {
	t.Helper()
	listener, err := net.Listen("tcp4", net.JoinHostPort(address.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func assertHTTPBody(t *testing.T, client *http.Client, address netip.Addr, port int, want string) {
	t.Helper()
	response, err := client.Get(fmt.Sprintf("http://%s/", net.JoinHostPort(address.String(), strconv.Itoa(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("body=%q, want %q", body, want)
	}
}
