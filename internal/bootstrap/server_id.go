package bootstrap

import (
	"context"

	"github.com/google/uuid"

	"remlink/internal/database"
)

const serverIDSetting = "server_id"

// EnsureServerID returns the stable Server UUID stored in SQLite.
func EnsureServerID(ctx context.Context, store *database.Store) (string, error) {
	value, found, err := database.GetSetting(ctx, store.DB(), serverIDSetting)
	if err != nil {
		return "", err
	}
	if found {
		if _, err := uuid.Parse(value); err != nil {
			return "", err
		}
		return value, nil
	}
	value = uuid.NewString()
	if err := database.SetSetting(ctx, store.DB(), serverIDSetting, value); err != nil {
		return "", err
	}
	return value, nil
}
