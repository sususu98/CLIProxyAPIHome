package main

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
)

type startupConfigRepository interface {
	LoadConfigAsRuntimeConfig(context.Context) (*config.Config, []byte, error)
	MaxEventID(context.Context) (int64, error)
}

func loadInitialRuntimeConfig(ctx context.Context, repo startupConfigRepository) (int64, *config.Config, error) {
	if repo == nil {
		return 0, nil, fmt.Errorf("startup config repository is unavailable")
	}
	highWater, errHighWater := repo.MaxEventID(ctx)
	if errHighWater != nil {
		return 0, nil, fmt.Errorf("get startup event high-water: %w", errHighWater)
	}
	cfg, _, errConfig := repo.LoadConfigAsRuntimeConfig(ctx)
	if errConfig != nil {
		return 0, nil, fmt.Errorf("load runtime config after event high-water: %w", errConfig)
	}
	return highWater, cfg, nil
}
