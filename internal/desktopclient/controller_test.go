package desktopclient

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"remlink/internal/clientsettings"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
)

type testProtector struct{}

func (testProtector) Protect(raw []byte) ([]byte, error)   { return append([]byte{}, raw...), nil }
func (testProtector) Unprotect(raw []byte) ([]byte, error) { return append([]byte{}, raw...), nil }

func testController(t *testing.T) (*Controller, *clientsettings.Store) {
	t.Helper()
	root := t.TempDir()
	settings, _ := clientsettings.NewStore(filepath.Join(root, "settings.json"), model.NodeTypeEngineer, testProtector{})
	identityStore, _ := identity.NewStore(filepath.Join(root, "identity.json"), testProtector{})
	controller := New(nodeagent.Options{NodeType: model.NodeTypeEngineer}, settings, identityStore, nil, nil, nil)
	t.Cleanup(controller.Close)
	return controller, settings
}

func receiveRun(t *testing.T, calls <-chan nodeagent.Options) nodeagent.Options {
	t.Helper()
	select {
	case options := <-calls:
		return options
	case <-time.After(time.Second):
		t.Fatal("saved settings did not start a connection")
		return nodeagent.Options{}
	}
}

func TestFirstLaunchWaitsForSettingsThenConnectsAndRestartsSerially(t *testing.T) {
	controller, store := testController(t)
	calls := make(chan nodeagent.Options, 4)
	var active, peak, latencyCalls atomic.Int32
	controller.options.OnLatency = func(time.Duration) { latencyCalls.Add(1) }
	controller.run = func(ctx context.Context, options nodeagent.Options) error {
		count := active.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		defer active.Add(-1)
		calls <- options
		options.OnOverlayReady(netip.MustParseAddr("10.88.0.2"))
		options.OnControlState(true)
		<-ctx.Done()
		return ctx.Err()
	}
	controller.Start(context.Background())
	if controller.Snapshot().Configured || len(calls) != 0 {
		t.Fatal("fresh launch contacted the Server without settings")
	}
	value := clientsettings.Defaults(model.NodeTypeEngineer)
	value.ServerURL, value.NodeName, value.JoinToken = "https://example.test", "first-name", "valid-join"
	if err := controller.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	first := receiveRun(t, calls)
	if first.ServerURL != value.ServerURL || first.NodeName != value.NodeName || first.JoinToken != value.JoinToken {
		t.Fatal("runtime did not receive saved settings")
	}
	value.NodeName = "new-name"
	if err := controller.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	second := receiveRun(t, calls)
	if second.NodeName != "new-name" || peak.Load() != 1 {
		t.Fatal("settings update overlapped old and new runtimes")
	}
	first.OnLatency(time.Millisecond)
	second.OnLatency(time.Millisecond)
	if latencyCalls.Load() != 1 {
		t.Fatal("a stopped runtime updated the new connection state")
	}
	loaded, exists, err := store.Load()
	if err != nil || !exists || loaded.NodeName != "new-name" {
		t.Fatal("settings update was not saved")
	}
	if err := controller.RenameNode("device-page-name"); err != nil {
		t.Fatal(err)
	}
	if receiveRun(t, calls).NodeName != "device-page-name" {
		t.Fatal("device name edit did not reconnect using the new name")
	}
}

func TestSavedSettingsAutoConnectAndFailureRemainsVisible(t *testing.T) {
	controller, store := testController(t)
	value := clientsettings.Defaults(model.NodeTypeEngineer)
	value.ServerURL, value.NodeName, value.JoinToken = "https://example.test", "saved-name", "valid-join"
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	called := make(chan nodeagent.Options, 1)
	controller.run = func(_ context.Context, options nodeagent.Options) error {
		called <- options
		return errors.New("服务器连接失败")
	}
	controller.Start(context.Background())
	var failedOptions nodeagent.Options
	select {
	case failedOptions = <-called:
	case <-time.After(time.Second):
		t.Fatal("saved settings did not auto-connect")
	}
	deadline := time.Now().Add(time.Second)
	for controller.Snapshot().Connecting && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state := controller.Snapshot()
	if !state.Configured || state.ConnectionError != "服务器连接失败" || state.ServerConnected {
		t.Fatalf("failure was hidden or settings lost: %+v", state)
	}
	failedOptions.OnOverlayReady(netip.MustParseAddr("10.88.0.99"))
	failedOptions.OnControlState(true)
	if controller.Snapshot().ServerConnected || controller.Snapshot().ControlConnected {
		t.Fatal("late callbacks made a failed runtime appear connected")
	}
}

func TestMissingJoinTokenDoesNotPersistOrConnect(t *testing.T) {
	controller, store := testController(t)
	controller.Start(context.Background())
	value := clientsettings.Defaults(model.NodeTypeEngineer)
	value.ServerURL, value.NodeName = "https://example.test", "new-name"
	if err := controller.SaveSettings(value); err == nil {
		t.Fatal("first registration accepted an empty Join Token")
	}
	if _, exists, _ := store.Load(); exists {
		t.Fatal("invalid first settings were persisted")
	}
}
