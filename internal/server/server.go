package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	authorizationv1 "github.com/agynio/apps/.gen/go/agynio/api/authorization/v1"
	identityv1 "github.com/agynio/apps/.gen/go/agynio/api/identity/v1"
	zitimanagementv1 "github.com/agynio/apps/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/apps/internal/store"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	clusterObject      = "cluster:global"
	clusterAdminAction = "admin"
	clusterWriteAction = "writer"
	identityMetadata   = "x-identity-id"
)

type Server struct {
	appsv1.UnimplementedAppsServiceServer
	store                 *store.Store
	identityClient        identityv1.IdentityServiceClient
	authorizationClient   authorizationv1.AuthorizationServiceClient
	zitiManagementClient  zitimanagementv1.ZitiManagementServiceClient
}

func New(
	store *store.Store,
	identityClient identityv1.IdentityServiceClient,
	authorizationClient authorizationv1.AuthorizationServiceClient,
	zitiManagementClient zitimanagementv1.ZitiManagementServiceClient,
) *Server {
	return &Server{
		store:                store,
		identityClient:       identityClient,
		authorizationClient:  authorizationClient,
		zitiManagementClient: zitiManagementClient,
	}
}

func (s *Server) RegisterApp(ctx context.Context, req *appsv1.RegisterAppRequest) (*appsv1.RegisterAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	if err := s.requireClusterAdmin(ctx, callerID); err != nil {
		return nil, err
	}

	slug := req.GetSlug()
	if err := validateSlug(slug); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
	}
	name := req.GetName()
	if err := validateName(name); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "name: %v", err)
	}

	appID := uuid.New()
	identityID := uuid.New()
	serviceToken, tokenHash, err := newServiceToken()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate service token: %v", err)
	}

	if _, err := s.identityClient.RegisterIdentity(ctx, &identityv1.RegisterIdentityRequest{
		IdentityId:   identityID.String(),
		IdentityType: identityv1.IdentityType_IDENTITY_TYPE_APP,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "register identity: %v", err)
	}

	zitiResp, err := s.zitiManagementClient.CreateAppIdentity(ctx, &zitimanagementv1.CreateAppIdentityRequest{
		IdentityId: identityID.String(),
		Slug:       slug,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create ziti identity: %v", err)
	}

	if err := s.writeAppAuthorization(ctx, identityID); err != nil {
		s.cleanupZitiIdentity(ctx, zitiResp.GetZitiIdentityId(), zitiResp.GetZitiServiceId())
		return nil, err
	}

	app, err := s.store.CreateApp(ctx, store.CreateAppInput{
		ID:               appID,
		Slug:             slug,
		Name:             name,
		Description:      req.GetDescription(),
		Icon:             req.GetIcon(),
		IdentityID:       identityID,
		ServiceTokenHash: tokenHash,
		ZitiIdentityID:   zitiResp.GetZitiIdentityId(),
		ZitiServiceID:    zitiResp.GetZitiServiceId(),
	})
	if err != nil {
		s.cleanupZitiIdentity(ctx, zitiResp.GetZitiIdentityId(), zitiResp.GetZitiServiceId())
		s.cleanupAuthorization(ctx, identityID)
		return nil, toStatusError(err)
	}

	return &appsv1.RegisterAppResponse{App: toProtoApp(app), ServiceToken: serviceToken}, nil
}

func (s *Server) GetApp(ctx context.Context, req *appsv1.GetAppRequest) (*appsv1.GetAppResponse, error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	app, err := s.store.GetApp(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.GetAppResponse{App: toProtoApp(app)}, nil
}

func (s *Server) GetAppBySlug(ctx context.Context, req *appsv1.GetAppBySlugRequest) (*appsv1.GetAppBySlugResponse, error) {
	slug := req.GetSlug()
	if err := validateSlug(slug); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
	}
	app, err := s.store.GetAppBySlug(ctx, slug)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.GetAppBySlugResponse{App: toProtoApp(app)}, nil
}

func (s *Server) ListApps(ctx context.Context, req *appsv1.ListAppsRequest) (*appsv1.ListAppsResponse, error) {
	apps, nextToken, err := s.store.ListApps(ctx, int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		var invalidToken *store.InvalidPageTokenError
		if errors.As(err, &invalidToken) {
			return nil, status.Errorf(codes.InvalidArgument, "invalid page_token: %v", invalidToken.Err)
		}
		return nil, status.Errorf(codes.Internal, "list apps: %v", err)
	}
	protoApps := make([]*appsv1.App, 0, len(apps))
	for _, app := range apps {
		protoApps = append(protoApps, toProtoApp(app))
	}
	return &appsv1.ListAppsResponse{Apps: protoApps, NextPageToken: nextToken}, nil
}

func (s *Server) DeleteApp(ctx context.Context, req *appsv1.DeleteAppRequest) (*appsv1.DeleteAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	if err := s.requireClusterAdmin(ctx, callerID); err != nil {
		return nil, err
	}

	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	app, err := s.store.GetApp(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}

	s.cleanupZitiIdentity(ctx, app.ZitiIdentityID, app.ZitiServiceID)
	s.cleanupAuthorization(ctx, app.IdentityID)

	if err := s.store.DeleteApp(ctx, id); err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.DeleteAppResponse{}, nil
}

func (s *Server) GetAppProfile(ctx context.Context, req *appsv1.GetAppProfileRequest) (*appsv1.GetAppProfileResponse, error) {
	identityID, err := parseUUID(req.GetIdentityId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "identity_id: %v", err)
	}
	app, err := s.store.GetAppByIdentityID(ctx, identityID)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.GetAppProfileResponse{Profile: toProtoAppProfile(app)}, nil
}

func (s *Server) ValidateServiceToken(ctx context.Context, req *appsv1.ValidateServiceTokenRequest) (*appsv1.ValidateServiceTokenResponse, error) {
	tokenHash := req.GetTokenHash()
	if tokenHash == "" {
		return nil, status.Error(codes.InvalidArgument, "token_hash must be provided")
	}
	app, err := s.store.GetAppByServiceTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.ValidateServiceTokenResponse{App: toProtoApp(app)}, nil
}

func (s *Server) requireClusterAdmin(ctx context.Context, identityID uuid.UUID) error {
	resp, err := s.authorizationClient.Check(ctx, &authorizationv1.CheckRequest{
		TupleKey: &authorizationv1.TupleKey{
			User:     fmt.Sprintf("identity:%s", identityID.String()),
			Relation: clusterAdminAction,
			Object:   clusterObject,
		},
	})
	if err != nil {
		return status.Errorf(codes.Internal, "authorization check: %v", err)
	}
	if !resp.GetAllowed() {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func (s *Server) writeAppAuthorization(ctx context.Context, identityID uuid.UUID) error {
	_, err := s.authorizationClient.Write(ctx, &authorizationv1.WriteRequest{
		Writes: []*authorizationv1.TupleKey{
			{
				User:     fmt.Sprintf("identity:%s", identityID.String()),
				Relation: clusterWriteAction,
				Object:   clusterObject,
			},
		},
	})
	if err != nil {
		return status.Errorf(codes.Internal, "authorization write: %v", err)
	}
	return nil
}

func (s *Server) cleanupAuthorization(ctx context.Context, identityID uuid.UUID) {
	_, _ = s.authorizationClient.Write(ctx, &authorizationv1.WriteRequest{
		Deletes: []*authorizationv1.TupleKey{
			{
				User:     fmt.Sprintf("identity:%s", identityID.String()),
				Relation: clusterWriteAction,
				Object:   clusterObject,
			},
		},
	})
}

func (s *Server) cleanupZitiIdentity(ctx context.Context, zitiIdentityID string, zitiServiceID string) {
	_, _ = s.zitiManagementClient.DeleteAppIdentity(ctx, &zitimanagementv1.DeleteAppIdentityRequest{
		ZitiIdentityId: zitiIdentityID,
		ZitiServiceId:  zitiServiceID,
	})
}

func identityFromMetadata(ctx context.Context) (uuid.UUID, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return uuid.UUID{}, fmt.Errorf("metadata missing")
	}
	values := md.Get(identityMetadata)
	if len(values) != 1 {
		return uuid.UUID{}, fmt.Errorf("expected single value")
	}
	value := values[0]
	if value == "" {
		return uuid.UUID{}, fmt.Errorf("value is empty")
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, err
	}
	return parsed, nil
}

func newServiceToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buffer)
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func parseUUID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.UUID{}, fmt.Errorf("value is empty")
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, err
	}
	return id, nil
}

func toStatusError(err error) error {
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return status.Error(codes.NotFound, notFound.Error())
	}
	var exists *store.AlreadyExistsError
	if errors.As(err, &exists) {
		return status.Error(codes.AlreadyExists, exists.Error())
	}
	return status.Errorf(codes.Internal, "internal error: %v", err)
}
