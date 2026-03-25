package config

import (
	"fmt"
	"os"
)

type Config struct {
	GRPCAddress              string
	DatabaseURL              string
	IdentityGRPCTarget       string
	AuthorizationGRPCTarget  string
	ZitiManagementGRPCTarget string
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
	return cfg, nil
}
