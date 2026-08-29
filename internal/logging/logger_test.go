package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModulesMatchV1Taxonomy(t *testing.T) {
	t.Parallel()
	if got, want := len(Modules), 11; got != want {
		t.Fatalf("Modules length = %d, want %d", got, want)
	}
	seen := make(map[Module]struct{}, len(Modules))
	for _, module := range Modules {
		if !module.Valid() {
			t.Fatalf("listed module %q is invalid", module)
		}
		if _, duplicate := seen[module]; duplicate {
			t.Fatalf("duplicate module %q", module)
		}
		seen[module] = struct{}{}
	}
}

func TestLoggerWritesStructuredModuleAndClosesIdempotently(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "remlink.log")
	var console bytes.Buffer
	config := DefaultConfig(path)
	config.Console = &console
	logger, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	core, err := logger.For(ModuleCore)
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	core.Info("started", slog.String("version", "test"))
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, `"module":"CORE"`) || !strings.Contains(text, `"msg":"started"`) {
		t.Fatalf("structured log missing expected fields: %s", text)
	}
	if console.Len() == 0 {
		t.Fatal("console writer received no log record")
	}
}

func TestPacketSamplingDefaultsOffAndRateLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	disabledConfig := DefaultConfig(filepath.Join(t.TempDir(), "disabled.log"))
	disabledConfig.Console = nil
	disabled, err := New(disabledConfig)
	if err != nil {
		t.Fatalf("New(disabled) error = %v", err)
	}
	t.Cleanup(func() { _ = disabled.Close() })
	logged, err := disabled.SamplePacketDebug(ctx, ModuleSubnet, "invalid-header", "packet rejected")
	if err != nil || logged {
		t.Fatalf("disabled SamplePacketDebug() = (%v, %v), want (false, nil)", logged, err)
	}

	enabledConfig := DefaultConfig(filepath.Join(t.TempDir(), "enabled.log"))
	enabledConfig.Console = nil
	enabledConfig.Level = slog.LevelDebug
	enabledConfig.Packets.Enabled = true
	enabledConfig.Packets.SampleInterval = time.Hour
	enabled, err := New(enabledConfig)
	if err != nil {
		t.Fatalf("New(enabled) error = %v", err)
	}
	t.Cleanup(func() { _ = enabled.Close() })
	first, err := enabled.SamplePacketDebug(ctx, ModuleSubnet, "invalid-header", "packet rejected")
	if err != nil || !first {
		t.Fatalf("first SamplePacketDebug() = (%v, %v), want (true, nil)", first, err)
	}
	second, err := enabled.SamplePacketDebug(ctx, ModuleSubnet, "invalid-header", "packet rejected")
	if err != nil || second {
		t.Fatalf("second SamplePacketDebug() = (%v, %v), want (false, nil)", second, err)
	}
	different, err := enabled.SamplePacketDebug(ctx, ModuleSubnet, "source-mismatch", "packet rejected")
	if err != nil || !different {
		t.Fatalf("different-key SamplePacketDebug() = (%v, %v), want (true, nil)", different, err)
	}
}

func TestSecurityWarningIsAlwaysRateLimited(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "security.log")
	config := DefaultConfig(path)
	config.Console = nil
	logger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	first, err := logger.SampleSecurityWarning(context.Background(), ModuleSubnet, "invalid-datagram", "packet rejected")
	if err != nil || !first {
		t.Fatalf("first warning = (%v, %v)", first, err)
	}
	second, err := logger.SampleSecurityWarning(context.Background(), ModuleSubnet, "invalid-datagram", "packet rejected")
	if err != nil || second {
		t.Fatalf("second warning = (%v, %v)", second, err)
	}
}

func TestLoggerRejectsUnknownModule(t *testing.T) {
	t.Parallel()
	config := DefaultConfig(filepath.Join(t.TempDir(), "remlink.log"))
	config.Console = nil
	logger, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	if _, err := logger.For(Module("PACKET")); !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("For() error = %v, want %v", err, ErrInvalidModule)
	}
}
