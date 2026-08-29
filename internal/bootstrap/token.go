package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync"

	"remlink/internal/database"
)

const joinTokenSetting = "join_token"

// JoinTokens maintains the single rotatable enrollment secret in settings.
type JoinTokens struct {
	mu sync.Mutex
	db databaseSettings
}

type databaseSettings interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string) error
}

type settingsAdapter struct{ store *database.Store }

func (a settingsAdapter) Get(ctx context.Context, key string) (string, bool, error) {
	return database.GetSetting(ctx, a.store.DB(), key)
}

func (a settingsAdapter) Set(ctx context.Context, key, value string) error {
	return database.SetSetting(ctx, a.store.DB(), key, value)
}

// NewJoinTokens binds token management to Server settings.
func NewJoinTokens(store *database.Store) *JoinTokens {
	return &JoinTokens{db: settingsAdapter{store: store}}
}

// Ensure returns the current Join Token, generating it on first startup.
func (m *JoinTokens) Ensure(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, found, err := m.db.Get(ctx, joinTokenSetting)
	if err != nil {
		return "", err
	}
	if found {
		return value, nil
	}
	return m.rotateLocked(ctx)
}

// Rotate revokes the previous Join Token and returns a new one.
func (m *JoinTokens) Rotate(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rotateLocked(ctx)
}

// Verify compares a submitted token without data-dependent early exit.
func (m *JoinTokens) Verify(ctx context.Context, submitted string) (bool, error) {
	current, err := m.Ensure(ctx)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(current), []byte(submitted)) == 1, nil
}

func (m *JoinTokens) rotateLocked(ctx context.Context) (string, error) {
	value, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := m.db.Set(ctx, joinTokenSetting, value); err != nil {
		return "", err
	}
	return value, nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func newNodeToken() (plain string, hash []byte, err error) {
	plain, err = randomToken()
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256([]byte(plain))
	return plain, digest[:], nil
}

func nodeTokenMatches(storedHash []byte, submitted string) bool {
	if len(storedHash) != sha256.Size || submitted == "" {
		return false
	}
	digest := sha256.Sum256([]byte(submitted))
	return subtle.ConstantTimeCompare(storedHash, digest[:]) == 1
}
