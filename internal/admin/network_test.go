package admin

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"remlink/internal/bootstrap"
	"remlink/internal/database"
	"remlink/internal/ipam"
	"remlink/internal/model"
	"remlink/internal/overlay/serverwg"
	"remlink/internal/protocol"
)

func TestNetworkManagerRunsSevenStepMigration(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	for _, node := range []model.Node{
		{ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer", OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "key-a", NodeTokenHash: []byte("a")},
		{ID: "site", Type: model.NodeTypeSite, Name: "Site", OverlayIP: netip.MustParseAddr("10.88.0.3"), WGPublicKey: "key-b", NodeTokenHash: []byte("b")},
	} {
		if err := store.CreateNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	ipamManager, _ := ipam.New(store, netip.MustParsePrefix("10.88.0.0/24"), netip.MustParseAddr("10.88.0.1"))
	steps := make([]string, 0, 4)
	peers := &fakeAdminPeers{steps: &steps}
	bootstrapService := &fakeBootstrapNetwork{config: bootstrap.ServiceConfig{
		WGEndpoint: "203.0.113.4:51820", ControlURL: "ws://10.88.0.1:7001/control",
		OverlayCIDR: netip.MustParsePrefix("10.88.0.0/24"), ServerOverlayIP: netip.MustParseAddr("10.88.0.1"), ConfigVersion: 1,
	}}
	control := &fakeAdminControl{steps: &steps}
	sessions := &fakeSessionControl{}
	var rebound netip.Addr
	manager, err := NewNetworkManager(store, ipamManager, peers, bootstrapService, control, sessions, Network{
		OverlayCIDR: "10.88.0.0/24", ServerOverlayIP: "10.88.0.1", WireGuardPort: 51820,
		SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	}, func(address netip.Addr) error { steps = append(steps, "rebind"); rebound = address; return nil })
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(ctx, NetworkUpdate{
		OverlayCIDR: "10.99.0.0/24", ServerOverlayIP: "10.99.0.1", WireGuardPort: 51830,
		SessionUDPPort: 6300, MTU: 1400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ConfigVersion != 2 || !sessions.began || !sessions.ended || peers.address.String() != "10.99.0.1/24" || peers.port != 51830 {
		t.Fatalf("migration state updated=%+v sessions=%+v peers=%s:%d", updated, sessions, peers.address, peers.port)
	}
	if sessions.prefix.String() != "10.99.0.0/24" || sessions.mtu != 1400 || sessions.udpPort != 6300 {
		t.Fatalf("Session Manager network remained stale: prefix=%s mtu=%d udp=%d", sessions.prefix, sessions.mtu, sessions.udpPort)
	}
	engineer, _ := store.GetNode(ctx, "engineer")
	site, _ := store.GetNode(ctx, "site")
	if engineer.OverlayIP.String() != "10.99.0.2" || site.OverlayIP.String() != "10.99.0.3" {
		t.Fatalf("migrated addresses Engineer=%s Site=%s", engineer.OverlayIP, site.OverlayIP)
	}
	if rebound.String() != "10.99.0.1" || control.version != 2 || control.notifications != 2 || !control.resetAll {
		t.Fatalf("Control migration rebound=%s version=%d notifications=%d reset=%v", rebound, control.version, control.notifications, control.resetAll)
	}
	if bootstrapService.config.ControlURL != "ws://10.99.0.1:7001/control" || bootstrapService.config.WGEndpoint != "203.0.113.4:51830" {
		t.Fatalf("Bootstrap config = %+v", bootstrapService.config)
	}
	wantSteps := []string{"notify", "notify", "peers", "rebind"}
	if len(steps) != len(wantSteps) {
		t.Fatalf("migration steps = %v, want %v", steps, wantSteps)
	}
	for index := range wantSteps {
		if steps[index] != wantSteps[index] {
			t.Fatalf("migration steps = %v, want %v", steps, wantSteps)
		}
	}
	loaded, err := LoadStoredNetwork(ctx, store, Network{})
	if err != nil || loaded != updated {
		t.Fatalf("stored network = %+v, %v", loaded, err)
	}
}

func TestNetworkManagerRollsBackKernelWhenControlRebindFails(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	ipamManager, _ := ipam.New(store, netip.MustParsePrefix("10.88.0.0/24"), netip.MustParseAddr("10.88.0.1"))
	peers := &fakeAdminPeers{}
	bootstrapService := &fakeBootstrapNetwork{config: bootstrap.ServiceConfig{
		WGEndpoint: "203.0.113.4:51820", ControlURL: "ws://10.88.0.1:7001/control",
		OverlayCIDR: netip.MustParsePrefix("10.88.0.0/24"), ServerOverlayIP: netip.MustParseAddr("10.88.0.1"), ConfigVersion: 1,
	}}
	sessions := &fakeSessionControl{}
	manager, err := NewNetworkManager(store, ipamManager, peers, bootstrapService, &fakeAdminControl{}, sessions, Network{
		OverlayCIDR: "10.88.0.0/24", ServerOverlayIP: "10.88.0.1", WireGuardPort: 51820,
		SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	}, func(netip.Addr) error { return errors.New("address unavailable") })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(ctx, NetworkUpdate{
		OverlayCIDR: "10.99.0.0/24", ServerOverlayIP: "10.99.0.1", WireGuardPort: 51830,
		SessionUDPPort: 6300, MTU: 1400,
	}); err == nil {
		t.Fatal("network migration unexpectedly succeeded despite Control rebind failure")
	}
	if current := manager.Current(); current.ConfigVersion != 1 || current.OverlayCIDR != "10.88.0.0/24" {
		t.Fatalf("failed migration changed current config: %+v", current)
	}
	if peers.address.String() != "10.88.0.1/24" || peers.port != 51820 {
		t.Fatalf("failed migration left kernel config at %s:%d", peers.address, peers.port)
	}
	if sessions.prefix.String() != "10.88.0.0/24" || sessions.mtu != 1280 || sessions.udpPort != 6200 {
		t.Fatalf("failed migration did not restore Session Manager: %s mtu=%d udp=%d", sessions.prefix, sessions.mtu, sessions.udpPort)
	}
	if bootstrapService.config.OverlayCIDR.String() != "10.88.0.0/24" || bootstrapService.config.ConfigVersion != 1 {
		t.Fatalf("failed migration did not restore Bootstrap config: %+v", bootstrapService.config)
	}
	stored, err := LoadStoredNetwork(ctx, store, Network{})
	if err != nil || stored != manager.Current() {
		t.Fatalf("failed migration did not restore atomic database setting: stored=%+v current=%+v err=%v", stored, manager.Current(), err)
	}
}

func TestAdminNetworkRejectsExitNodeOverlay(t *testing.T) {
	_, _, err := validateNetwork(Network{
		OverlayCIDR: "0.0.0.0/0", ServerOverlayIP: "10.88.0.1",
		WireGuardPort: 51820, SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	})
	if err == nil {
		t.Fatal("Admin network accepted 0.0.0.0/0 Exit Node Overlay")
	}
}

type fakeAdminPeers struct {
	address netip.Prefix
	port    int
	peers   []serverwg.Peer
	steps   *[]string
	ensured netip.Addr
	removed int
}

func (f *fakeAdminPeers) EnsurePeer(_ context.Context, _ string, address netip.Addr) error {
	if f.steps != nil {
		*f.steps = append(*f.steps, "peers")
	}
	f.ensured = address
	return nil
}
func (f *fakeAdminPeers) RemovePeer(context.Context, string) error {
	f.removed++
	return nil
}
func (*fakeAdminPeers) LastHandshake(context.Context, string) (*time.Time, error) {
	value := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	return &value, nil
}
func (f *fakeAdminPeers) Reconfigure(_ context.Context, address netip.Prefix, port int, peers []serverwg.Peer) error {
	if f.steps != nil {
		*f.steps = append(*f.steps, "peers")
	}
	f.address, f.port, f.peers = address, port, peers
	return nil
}

type fakeBootstrapNetwork struct{ config bootstrap.ServiceConfig }

func (f *fakeBootstrapNetwork) NetworkSnapshot() bootstrap.ServiceConfig { return f.config }
func (f *fakeBootstrapNetwork) UpdateNetwork(config bootstrap.ServiceConfig) error {
	f.config = config
	return nil
}

type fakeAdminControl struct {
	version        uint64
	notifications  int
	resetAll       bool
	steps          *[]string
	resetNode      bool
	resetNodeCount int
	resetReason    string
}

func (f *fakeAdminControl) Send(_ context.Context, _ string, messageType protocol.ControlMessageType, _ any) error {
	if messageType == protocol.ControlRebootstrapRequired {
		f.notifications++
		if f.steps != nil {
			*f.steps = append(*f.steps, "notify")
		}
	}
	return nil
}
func (f *fakeAdminControl) SetNetworkConfigVersion(version uint64) error {
	f.version = version
	return nil
}
func (f *fakeAdminControl) ResetNodeConnection(_, reason string) {
	f.resetNode, f.resetReason = true, reason
	f.resetNodeCount++
}
func (f *fakeAdminControl) ResetConnections(string) { f.resetAll = true }

type fakeSessionControl struct {
	all             bool
	began           bool
	ended           bool
	prefix          netip.Prefix
	mtu             int
	udpPort         int
	nodeDisconnects int
	nodeReason      string
}

func (*fakeSessionControl) Disconnect(context.Context, uint64, string) error { return nil }
func (f *fakeSessionControl) DisconnectAll(context.Context, string) error    { f.all = true; return nil }

func (f *fakeSessionControl) DisconnectNode(_ context.Context, _, reason string) error {
	f.nodeDisconnects++
	f.nodeReason = reason
	return nil
}
func (f *fakeSessionControl) ReconfigureNetwork(prefix netip.Prefix, mtu, udpPort int) error {
	f.prefix, f.mtu, f.udpPort = prefix, mtu, udpPort
	return nil
}
func (f *fakeSessionControl) BeginNetworkMigration(context.Context, string) error {
	f.began = true
	return nil
}
func (f *fakeSessionControl) EndNetworkMigration() { f.ended = true }
