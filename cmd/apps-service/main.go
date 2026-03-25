package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	authorizationv1 "github.com/agynio/apps/.gen/go/agynio/api/authorization/v1"
	identityv1 "github.com/agynio/apps/.gen/go/agynio/api/identity/v1"
	zitimanagementv1 "github.com/agynio/apps/.gen/go/agynio/api/ziti_management/v1"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/agynio/apps/internal/config"
	"github.com/agynio/apps/internal/db"
	"github.com/agynio/apps/internal/server"
	"github.com/agynio/apps/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("apps-service: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("create connection pool: %w", err)
	}
	defer pool.Close()

	if err := db.ApplyMigrations(ctx, pool); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	identityConn, err := grpc.DialContext(ctx, cfg.IdentityGRPCTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("connect to identity: %w", err)
	}
	defer identityConn.Close()

	authConn, err := grpc.DialContext(ctx, cfg.AuthorizationGRPCTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("connect to authorization: %w", err)
	}
	defer authConn.Close()

	zitiConn, err := grpc.DialContext(ctx, cfg.ZitiManagementGRPCTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("connect to ziti management: %w", err)
	}
	defer zitiConn.Close()

	grpcServer := grpc.NewServer()
	serverInstance := server.New(
		store.New(pool),
		identityv1.NewIdentityServiceClient(identityConn),
		authorizationv1.NewAuthorizationServiceClient(authConn),
		zitimanagementv1.NewZitiManagementServiceClient(zitiConn),
	)
	appsv1.RegisterAppsServiceServer(grpcServer, serverInstance)

	lis, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.GRPCAddress, err)
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	log.Printf("AppsService listening on %s", cfg.GRPCAddress)

	if err := grpcServer.Serve(lis); err != nil {
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
