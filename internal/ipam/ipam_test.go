package ipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"

	"remlink/internal/database"
	"remlink/internal/model"
)

func TestReserveTenNodesUniqueStableAndConcurrent(t *testing.T) {
	manager, store := testManager(t, "10.88.0.0/16", "10.88.0.1")
	ctx := context.Background()
	const count = 10
	results := make(chan model.Node, count)
	errorsChannel := make(chan error, count)
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			node, created, err := manager.ReserveNode(ctx, newNode(index))
			if err != nil {
				errorsChannel <- err
				return
			}
			if !created {
				errorsChannel <- fmt.Errorf("node %d was not newly created", index)
				return
			}
			results <- node
		}()
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	seen := make(map[netip.Addr]struct{}, count)
	for result := range results {
		if result.OverlayIP == netip.MustParseAddr("10.88.0.1") {
			t.Fatal("Server address allocated to node")
		}
		if _, duplicate := seen[result.OverlayIP]; duplicate {
			t.Fatalf("duplicate allocation %s", result.OverlayIP)
		}
		seen[result.OverlayIP] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("unique allocation count = %d, want %d", len(seen), count)
	}

	existing, err := store.GetNode(ctx, newNode(3).ID)
	if err != nil {
		t.Fatal(err)
	}
	restarted, created, err := manager.ReserveNode(ctx, newNode(3))
	if err != nil || created || restarted.OverlayIP != existing.OverlayIP {
		t.Fatalf("restart allocation = %v, created=%v, err=%v; want %s", restarted.OverlayIP, created, err, existing.OverlayIP)
	}
}

func TestManualChangeAndRelease(t *testing.T) {
	manager, store := testManager(t, "10.88.0.0/24", "10.88.0.1")
	ctx := context.Background()
	first, _, err := manager.ReserveNode(ctx, newNode(1))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := manager.ReserveNode(ctx, newNode(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ChangeNodeAddress(ctx, first.ID, second.OverlayIP); !errors.Is(err, ErrAddressUnavailable) {
		t.Fatalf("occupied address error = %v", err)
	}
	for _, reserved := range []string{"10.88.0.0", "10.88.0.1", "10.88.0.255", "10.89.0.2"} {
		if err := manager.ChangeNodeAddress(ctx, first.ID, netip.MustParseAddr(reserved)); !errors.Is(err, ErrAddressUnavailable) {
			t.Errorf("reserved address %s error = %v", reserved, err)
		}
	}
	desired := netip.MustParseAddr("10.88.0.100")
	if err := manager.ChangeNodeAddress(ctx, first.ID, desired); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetNode(ctx, first.ID)
	if err != nil || updated.OverlayIP != desired {
		t.Fatalf("updated address = %s, err=%v", updated.OverlayIP, err)
	}
	if err := manager.ReleaseNode(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNode(ctx, first.ID); !errors.Is(err, database.ErrNodeNotFound) {
		t.Fatalf("released node error = %v", err)
	}
}

func TestPoolExhaustion(t *testing.T) {
	manager, _ := testManager(t, "10.88.0.0/30", "10.88.0.1")
	ctx := context.Background()
	if _, _, err := manager.ReserveNode(ctx, newNode(1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.ReserveNode(ctx, newNode(2)); !errors.Is(err, ErrAddressPoolExhausted) {
		t.Fatalf("second /30 allocation error = %v", err)
	}
}

func TestIPAMRejectsExitNodePool(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := New(database.NewStore(db), netip.MustParsePrefix("0.0.0.0/0"), netip.MustParseAddr("10.88.0.1")); err == nil {
		t.Fatal("IPAM accepted 0.0.0.0/0 Exit Node pool")
	}
}

func testManager(t *testing.T, prefix, server string) (*Manager, *database.Store) {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "remlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	manager, err := New(store, netip.MustParsePrefix(prefix), netip.MustParseAddr(server))
	if err != nil {
		t.Fatal(err)
	}
	return manager, store
}

func newNode(index int) model.Node {
	return model.Node{
		ID:            fmt.Sprintf("00000000-0000-4000-8000-%012d", index),
		Type:          model.NodeTypeEngineer,
		Name:          fmt.Sprintf("node-%d", index),
		WGPublicKey:   fmt.Sprintf("public-key-%d", index),
		NodeTokenHash: []byte(fmt.Sprintf("token-hash-%d", index)),
	}
}
