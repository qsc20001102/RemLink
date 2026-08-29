package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const defaultPacketSampleInterval = 5 * time.Second

var ErrInvalidModule = errors.New("invalid logging module")

// RotationConfig controls rolling-file retention.
type RotationConfig struct {
	MaxSizeMB  int
	MaxAgeDays int
	MaxBackups int
	LocalTime  bool
	Compress   bool
}

// PacketConfig controls opt-in, rate-limited packet-event metadata logging.
// Raw packet payloads are deliberately not accepted by the sampling API.
type PacketConfig struct {
	Enabled        bool
	SampleInterval time.Duration
}

// Config controls the slog handler and rolling file.
type Config struct {
	File     string
	Level    slog.Level
	JSON     bool
	Console  io.Writer
	Rotation RotationConfig
	Packets  PacketConfig
}

// DefaultConfig returns production-safe defaults with packet events disabled.
func DefaultConfig(file string) Config {
	return Config{
		File:    file,
		Level:   slog.LevelInfo,
		JSON:    true,
		Console: os.Stderr,
		Rotation: RotationConfig{
			MaxSizeMB:  50,
			MaxAgeDays: 14,
			MaxBackups: 5,
			LocalTime:  true,
			Compress:   true,
		},
		Packets: PacketConfig{
			Enabled:        false,
			SampleInterval: defaultPacketSampleInterval,
		},
	}
}

// Logger owns the structured logger and its rolling file writer.
type Logger struct {
	base     *slog.Logger
	file     *lumberjack.Logger
	packets  packetSampler
	security packetSampler
}

// New constructs a structured logger writing to a rolling file and optionally a console.
func New(config Config) (*Logger, error) {
	if config.File == "" {
		return nil, fmt.Errorf("log file path must not be empty")
	}
	applyRotationDefaults(&config.Rotation)
	if config.Packets.SampleInterval <= 0 {
		config.Packets.SampleInterval = defaultPacketSampleInterval
	}
	if err := os.MkdirAll(filepath.Dir(config.File), 0o750); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	rollingFile := &lumberjack.Logger{
		Filename:   config.File,
		MaxSize:    config.Rotation.MaxSizeMB,
		MaxAge:     config.Rotation.MaxAgeDays,
		MaxBackups: config.Rotation.MaxBackups,
		LocalTime:  config.Rotation.LocalTime,
		Compress:   config.Rotation.Compress,
	}
	var writer io.Writer = rollingFile
	if config.Console != nil {
		writer = io.MultiWriter(config.Console, rollingFile)
	}

	handlerOptions := &slog.HandlerOptions{Level: config.Level}
	var handler slog.Handler
	if config.JSON {
		handler = slog.NewJSONHandler(writer, handlerOptions)
	} else {
		handler = slog.NewTextHandler(writer, handlerOptions)
	}

	logger := &Logger{
		base: slog.New(handler),
		file: rollingFile,
	}
	logger.packets = packetSampler{
		enabled:  config.Packets.Enabled,
		interval: config.Packets.SampleInterval,
		last:     make(map[packetSampleKey]time.Time),
	}
	logger.security = packetSampler{
		enabled: true, interval: defaultPacketSampleInterval,
		last: make(map[packetSampleKey]time.Time),
	}
	return logger, nil
}

// For returns a logger permanently tagged with a validated module.
func (l *Logger) For(module Module) (*slog.Logger, error) {
	if !module.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidModule, module)
	}
	return l.base.With(slog.String("module", string(module))), nil
}

// SamplePacketDebug emits at most one DEBUG metadata record per module/key interval.
// It returns true only when the sample passed the limiter. Callers must never add raw
// packet bytes to attrs.
func (l *Logger) SamplePacketDebug(
	ctx context.Context,
	module Module,
	key string,
	message string,
	attrs ...slog.Attr,
) (bool, error) {
	if !module.Valid() {
		return false, fmt.Errorf("%w: %q", ErrInvalidModule, module)
	}
	if !l.packets.allow(module, key, time.Now()) {
		return false, nil
	}
	logger := l.base.With(
		slog.String("module", string(module)),
		slog.String("packet_sample", key),
	)
	logger.LogAttrs(ctx, slog.LevelDebug, message, attrs...)
	return true, nil
}

// SampleSecurityWarning emits rate-limited metadata for rejected or dropped
// packets. It is always active because these are security/diagnostic events,
// while routine per-packet DEBUG sampling remains opt-in. Raw payload bytes are
// deliberately not accepted by this API.
func (l *Logger) SampleSecurityWarning(
	ctx context.Context,
	module Module,
	key string,
	message string,
	attrs ...slog.Attr,
) (bool, error) {
	if !module.Valid() {
		return false, fmt.Errorf("%w: %q", ErrInvalidModule, module)
	}
	if !l.security.allow(module, key, time.Now()) {
		return false, nil
	}
	logger := l.base.With(
		slog.String("module", string(module)),
		slog.String("security_sample", key),
	)
	logger.LogAttrs(ctx, slog.LevelWarn, message, attrs...)
	return true, nil
}

// Close flushes and closes the rolling file. It is safe to call more than once.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func applyRotationDefaults(config *RotationConfig) {
	if config.MaxSizeMB <= 0 {
		config.MaxSizeMB = 50
	}
	if config.MaxAgeDays <= 0 {
		config.MaxAgeDays = 14
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = 5
	}
}

type packetSampleKey struct {
	module Module
	key    string
}

type packetSampler struct {
	mu       sync.Mutex
	enabled  bool
	interval time.Duration
	last     map[packetSampleKey]time.Time
}

func (s *packetSampler) allow(module Module, key string, now time.Time) bool {
	if !s.enabled {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sample := packetSampleKey{module: module, key: key}
	if previous, ok := s.last[sample]; ok && now.Sub(previous) < s.interval {
		return false
	}
	s.last[sample] = now
	return true
}
