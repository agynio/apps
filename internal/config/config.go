package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	GRPCAddress              string
	DatabaseURL              string
	IdentityGRPCTarget       string
	AuthorizationGRPCTarget  string
	ZitiManagementGRPCTarget string
	GroupsGRPCTarget         string
	NATSURL                  string
	GroupSyncDurable         string
	ReconciliationInterval   time.Duration
}

func FromEnv() (Config, error) {
	cfg := Config{}
	cfg.GRPCAddress = os.Getenv("GRPC_ADDRESS")
	if cfg.GRPCAddress == "" {
		cfg.GRPCAddress = ":50051"
	}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}
	cfg.IdentityGRPCTarget = os.Getenv("IDENTITY_GRPC_TARGET")
	if cfg.IdentityGRPCTarget == "" {
		cfg.IdentityGRPCTarget = "identity:50051"
	}
	cfg.AuthorizationGRPCTarget = os.Getenv("AUTHORIZATION_GRPC_TARGET")
	if cfg.AuthorizationGRPCTarget == "" {
		cfg.AuthorizationGRPCTarget = "authorization:50051"
	}
	cfg.ZitiManagementGRPCTarget = os.Getenv("ZITI_MANAGEMENT_GRPC_TARGET")
	if cfg.ZitiManagementGRPCTarget == "" {
		cfg.ZitiManagementGRPCTarget = "ziti-management:50051"
	}
	cfg.GroupsGRPCTarget = os.Getenv("GROUPS_GRPC_TARGET")
	if cfg.GroupsGRPCTarget == "" {
		cfg.GroupsGRPCTarget = "groups:50051"
	}
	cfg.NATSURL = os.Getenv("NATS_URL")
	if cfg.NATSURL == "" {
		cfg.NATSURL = "nats://nats:4222"
	}
	cfg.GroupSyncDurable = os.Getenv("GROUP_SYNC_DURABLE")
	if cfg.GroupSyncDurable == "" {
		cfg.GroupSyncDurable = "apps-group-sync"
	}
	reconciliationInterval := os.Getenv("GROUP_SYNC_RECONCILIATION_INTERVAL")
	if reconciliationInterval == "" {
		reconciliationInterval = "60s"
	}
	duration, err := time.ParseDuration(reconciliationInterval)
	if err != nil {
		return Config{}, fmt.Errorf("GROUP_SYNC_RECONCILIATION_INTERVAL: %w", err)
	}
	cfg.ReconciliationInterval = duration
	return cfg, nil
}
