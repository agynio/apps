package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"

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
	clusterObject            = "cluster:global"
	clusterAppWriterRelation = "writer"
	identityMetadata         = "x-identity-id"
)

var permissionToRelation = map[string]string{
	"thread:create":   "can_create_thread",
	"thread:write":    "can_write_thread",
	"participant:add": "can_add_participant",
}

type AppStore interface {
	CreateApp(ctx context.Context, input store.CreateAppInput) (store.App, error)
	UpdateApp(ctx context.Context, input store.UpdateAppInput) (store.App, error)
	GetApp(ctx context.Context, id uuid.UUID) (store.App, error)
	GetAppBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (store.App, error)
	GetAppByIdentityID(ctx context.Context, identityID uuid.UUID) (store.App, error)
	GetAppByServiceTokenHash(ctx context.Context, tokenHash string) (store.App, error)
	ListApps(ctx context.Context, pageSize int, pageToken string, filter store.ListAppsFilter) ([]store.App, string, error)
	DeleteApp(ctx context.Context, id uuid.UUID) error
	HasActiveInstallations(ctx context.Context, appID uuid.UUID) (bool, error)
	UpdateAppZitiIdentity(ctx context.Context, id uuid.UUID, zitiIdentityID string, zitiServiceID string) error
	CreateInstallation(ctx context.Context, input store.CreateInstallationInput) (store.Installation, error)
	GetInstallation(ctx context.Context, id uuid.UUID) (store.Installation, error)
	GetInstallationBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (store.Installation, error)
	ListInstallations(ctx context.Context, pageSize int, pageToken string, filter store.ListInstallationsFilter) ([]store.Installation, string, error)
	UpdateInstallation(ctx context.Context, input store.UpdateInstallationInput) (store.Installation, error)
	DeleteInstallation(ctx context.Context, id uuid.UUID) error
}

type Server struct {
	appsv1.UnimplementedAppsServiceServer
	store                AppStore
	identityClient       identityv1.IdentityServiceClient
	authorizationClient  authorizationv1.AuthorizationServiceClient
	zitiManagementClient zitimanagementv1.ZitiManagementServiceClient
}

func New(
	store AppStore,
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

func (s *Server) CreateApp(ctx context.Context, req *appsv1.CreateAppRequest) (*appsv1.CreateAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	organizationID, err := parseUUID(req.GetOrganizationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
	}
	if err := s.requireOrgOwner(ctx, callerID, organizationID); err != nil {
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
	visibility, err := toStoreVisibility(req.GetVisibility())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "visibility: %v", err)
	}
	permissions := req.GetPermissions()
	if err := validatePermissions(permissions); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "permissions: %v", err)
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

	if err := s.writeAppAuthorization(ctx, identityID); err != nil {
		// TODO: clean up orphaned identity once Identity service supports deletion.
		log.Printf("WARN: orphaned identity %s after authorization failure", identityID)
		return nil, err
	}

	zitiServiceResp, err := s.zitiManagementClient.CreateService(ctx, &zitimanagementv1.CreateServiceRequest{
		Name:           fmt.Sprintf("app-%s", slug),
		RoleAttributes: []string{"app-services"},
	})
	if err != nil {
		s.cleanupAuthorization(ctx, identityID)
		return nil, status.Errorf(codes.Internal, "create ziti service: %v", err)
	}

	app, err := s.store.CreateApp(ctx, store.CreateAppInput{
		ID:               appID,
		OrganizationID:   organizationID,
		Slug:             slug,
		Name:             name,
		Description:      req.GetDescription(),
		Icon:             req.GetIcon(),
		IdentityID:       identityID,
		ServiceTokenHash: tokenHash,
		ZitiIdentityID:   "",
		ZitiServiceID:    zitiServiceResp.GetZitiServiceId(),
		Visibility:       visibility,
		Permissions:      permissions,
	})
	if err != nil {
		s.cleanupZitiIdentity(ctx, identityID, zitiServiceResp.GetZitiServiceId())
		s.cleanupAuthorization(ctx, identityID)
		// TODO: clean up orphaned identity once Identity service supports deletion.
		log.Printf("WARN: orphaned identity %s after store failure", identityID)
		return nil, toStatusError(err)
	}

	return &appsv1.CreateAppResponse{App: toProtoApp(app), ServiceToken: serviceToken}, nil
}

func (s *Server) UpdateApp(ctx context.Context, req *appsv1.UpdateAppRequest) (*appsv1.UpdateAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	app, err := s.store.GetApp(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	if err := s.requireOrgOwner(ctx, callerID, app.OrganizationID); err != nil {
		return nil, err
	}

	input := store.UpdateAppInput{ID: id}
	if req.Name != nil {
		name := req.GetName()
		if err := validateName(name); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "name: %v", err)
		}
		input.Name = &name
	}
	if req.Description != nil {
		description := req.GetDescription()
		input.Description = &description
	}
	if req.Icon != nil {
		icon := req.GetIcon()
		input.Icon = &icon
	}
	if req.Visibility != nil {
		visibility, err := toStoreVisibility(req.GetVisibility())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "visibility: %v", err)
		}
		input.Visibility = &visibility
	}

	app, err = s.store.UpdateApp(ctx, input)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.UpdateAppResponse{App: toProtoApp(app)}, nil
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
	organizationID, err := parseUUID(req.GetOrganizationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
	}
	slug := req.GetSlug()
	if err := validateSlug(slug); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
	}
	app, err := s.store.GetAppBySlug(ctx, organizationID, slug)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.GetAppBySlugResponse{App: toProtoApp(app)}, nil
}

func (s *Server) ListApps(ctx context.Context, req *appsv1.ListAppsRequest) (*appsv1.ListAppsResponse, error) {
	filter := store.ListAppsFilter{}
	if req.GetOrganizationId() != "" {
		organizationID, err := parseUUID(req.GetOrganizationId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
		}
		filter.OrganizationID = &organizationID
	}
	if req.GetVisibility() != appsv1.AppVisibility_APP_VISIBILITY_UNSPECIFIED {
		visibility, err := toStoreVisibility(req.GetVisibility())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "visibility: %v", err)
		}
		filter.Visibility = &visibility
	}

	apps, nextToken, err := s.store.ListApps(ctx, int(req.GetPageSize()), req.GetPageToken(), filter)
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
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	app, err := s.store.GetApp(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	if err := s.requireOrgOwner(ctx, callerID, app.OrganizationID); err != nil {
		return nil, err
	}
	active, err := s.store.HasActiveInstallations(ctx, app.Meta.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check installations: %v", err)
	}
	if active {
		return nil, status.Error(codes.FailedPrecondition, "app has active installations")
	}

	if app.ZitiServiceID != "" {
		if err := s.deleteZitiIdentity(ctx, app.IdentityID, app.ZitiServiceID); err != nil {
			return nil, status.Errorf(codes.Internal, "delete ziti identity: %v", err)
		}
	}
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
	// NOTE: ValidateServiceTokenRequest.token_hash currently carries the raw service token.
	// The server hashes it until the proto field is renamed.
	token := req.GetTokenHash()
	if token == "" {
		return nil, status.Error(codes.InvalidArgument, "service_token must be provided")
	}
	tokenHash := hashServiceToken(token)
	app, err := s.store.GetAppByServiceTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.ValidateServiceTokenResponse{App: toProtoApp(app)}, nil
}

func (s *Server) EnrollApp(ctx context.Context, req *appsv1.EnrollAppRequest) (*appsv1.EnrollAppResponse, error) {
	token := req.GetServiceToken()
	if token == "" {
		return nil, status.Error(codes.InvalidArgument, "service_token must be provided")
	}
	tokenHash := hashServiceToken(token)
	app, err := s.store.GetAppByServiceTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, toStatusError(err)
	}

	zitiResp, err := s.zitiManagementClient.CreateAppIdentity(ctx, &zitimanagementv1.CreateAppIdentityRequest{
		IdentityId: app.IdentityID.String(),
		Slug:       app.Slug,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create ziti identity: %v", err)
	}

	if err := s.store.UpdateAppZitiIdentity(ctx, app.Meta.ID, zitiResp.GetZitiIdentityId(), app.ZitiServiceID); err != nil {
		s.cleanupZitiIdentity(ctx, app.IdentityID, app.ZitiServiceID)
		return nil, status.Errorf(codes.Internal, "update ziti identity: %v", err)
	}

	return &appsv1.EnrollAppResponse{
		IdentityJson: zitiResp.GetIdentityJson(),
		IdentityId:   app.IdentityID.String(),
	}, nil
}

func (s *Server) InstallApp(ctx context.Context, req *appsv1.InstallAppRequest) (*appsv1.InstallAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	appID, err := parseUUID(req.GetAppId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "app_id: %v", err)
	}
	organizationID, err := parseUUID(req.GetOrganizationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
	}
	if err := s.requireOrgOwner(ctx, callerID, organizationID); err != nil {
		return nil, err
	}
	app, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return nil, toStatusError(err)
	}
	if app.Visibility == store.AppVisibilityInternal && app.OrganizationID != organizationID {
		return nil, status.Error(codes.PermissionDenied, "app is internal to another organization")
	}

	slug := req.GetSlug()
	if slug == "" {
		slug = app.Slug
	}
	if err := validateSlug(slug); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
	}
	configuration := protoStructToMap(req.GetConfiguration())

	installation, err := s.store.CreateInstallation(ctx, store.CreateInstallationInput{
		ID:             uuid.New(),
		AppID:          app.Meta.ID,
		OrganizationID: organizationID,
		Slug:           slug,
		Configuration:  configuration,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	if err := s.writeInstallationTuples(ctx, app, organizationID); err != nil {
		if deleteErr := s.store.DeleteInstallation(ctx, installation.Meta.ID); deleteErr != nil {
			log.Printf("WARN: failed to rollback installation %s after authorization failure: %v", installation.Meta.ID, deleteErr)
		}
		return nil, err
	}

	protoInstallation, err := toProtoInstallation(installation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "convert installation: %v", err)
	}
	return &appsv1.InstallAppResponse{Installation: protoInstallation}, nil
}

func (s *Server) GetInstallation(ctx context.Context, req *appsv1.GetInstallationRequest) (*appsv1.GetInstallationResponse, error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	installation, err := s.store.GetInstallation(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	protoInstallation, err := toProtoInstallation(installation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "convert installation: %v", err)
	}
	return &appsv1.GetInstallationResponse{Installation: protoInstallation}, nil
}

func (s *Server) GetInstallationBySlug(ctx context.Context, req *appsv1.GetInstallationBySlugRequest) (*appsv1.GetInstallationBySlugResponse, error) {
	organizationID, err := parseUUID(req.GetOrganizationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
	}
	slug := req.GetSlug()
	if err := validateSlug(slug); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
	}
	installation, err := s.store.GetInstallationBySlug(ctx, organizationID, slug)
	if err != nil {
		return nil, toStatusError(err)
	}
	protoInstallation, err := toProtoInstallation(installation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "convert installation: %v", err)
	}
	return &appsv1.GetInstallationBySlugResponse{Installation: protoInstallation}, nil
}

func (s *Server) ListInstallations(ctx context.Context, req *appsv1.ListInstallationsRequest) (*appsv1.ListInstallationsResponse, error) {
	filter := store.ListInstallationsFilter{}
	if req.GetOrganizationId() != "" {
		organizationID, err := parseUUID(req.GetOrganizationId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
		}
		filter.OrganizationID = &organizationID
	}
	if req.GetAppId() != "" {
		appID, err := parseUUID(req.GetAppId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "app_id: %v", err)
		}
		filter.AppID = &appID
	}

	installations, nextToken, err := s.store.ListInstallations(ctx, int(req.GetPageSize()), req.GetPageToken(), filter)
	if err != nil {
		var invalidToken *store.InvalidPageTokenError
		if errors.As(err, &invalidToken) {
			return nil, status.Errorf(codes.InvalidArgument, "invalid page_token: %v", invalidToken.Err)
		}
		return nil, status.Errorf(codes.Internal, "list installations: %v", err)
	}
	protoInstallations := make([]*appsv1.Installation, 0, len(installations))
	for _, installation := range installations {
		protoInstallation, err := toProtoInstallation(installation)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "convert installation: %v", err)
		}
		protoInstallations = append(protoInstallations, protoInstallation)
	}
	return &appsv1.ListInstallationsResponse{Installations: protoInstallations, NextPageToken: nextToken}, nil
}

func (s *Server) UpdateInstallation(ctx context.Context, req *appsv1.UpdateInstallationRequest) (*appsv1.UpdateInstallationResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	installation, err := s.store.GetInstallation(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	if err := s.requireOrgOwner(ctx, callerID, installation.OrganizationID); err != nil {
		return nil, err
	}

	input := store.UpdateInstallationInput{ID: id}
	if req.Slug != nil {
		slug := req.GetSlug()
		if err := validateSlug(slug); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "slug: %v", err)
		}
		input.Slug = &slug
	}
	if req.Configuration != nil {
		configuration := protoStructToMap(req.GetConfiguration())
		input.Configuration = &configuration
	}

	installation, err = s.store.UpdateInstallation(ctx, input)
	if err != nil {
		return nil, toStatusError(err)
	}
	protoInstallation, err := toProtoInstallation(installation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "convert installation: %v", err)
	}
	return &appsv1.UpdateInstallationResponse{Installation: protoInstallation}, nil
}

func (s *Server) UninstallApp(ctx context.Context, req *appsv1.UninstallAppRequest) (*appsv1.UninstallAppResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	installation, err := s.store.GetInstallation(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	if err := s.requireOrgOwner(ctx, callerID, installation.OrganizationID); err != nil {
		return nil, err
	}
	app, err := s.store.GetApp(ctx, installation.AppID)
	if err != nil {
		return nil, toStatusError(err)
	}
	s.deleteInstallationTuples(ctx, app, installation.OrganizationID)
	if err := s.store.DeleteInstallation(ctx, installation.Meta.ID); err != nil {
		return nil, toStatusError(err)
	}
	return &appsv1.UninstallAppResponse{}, nil
}

func (s *Server) GetInstallationConfiguration(ctx context.Context, req *appsv1.GetInstallationConfigurationRequest) (*appsv1.GetInstallationConfigurationResponse, error) {
	callerID, err := identityFromMetadata(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
	}
	id, err := parseUUID(req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "id: %v", err)
	}
	installation, err := s.store.GetInstallation(ctx, id)
	if err != nil {
		return nil, toStatusError(err)
	}
	app, err := s.store.GetApp(ctx, installation.AppID)
	if err != nil {
		return nil, toStatusError(err)
	}
	if app.IdentityID != callerID {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	configuration, err := mapToProtoStruct(installation.Configuration)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "convert configuration: %v", err)
	}
	return &appsv1.GetInstallationConfigurationResponse{Configuration: configuration}, nil
}

func (s *Server) requireOrgOwner(ctx context.Context, identityID uuid.UUID, organizationID uuid.UUID) error {
	resp, err := s.authorizationClient.Check(ctx, &authorizationv1.CheckRequest{
		TupleKey: &authorizationv1.TupleKey{
			User:     fmt.Sprintf("identity:%s", identityID.String()),
			Relation: "owner",
			Object:   fmt.Sprintf("organization:%s", organizationID.String()),
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
				Relation: clusterAppWriterRelation,
				Object:   clusterObject,
			},
		},
	})
	if err != nil {
		return status.Errorf(codes.Internal, "authorization write: %v", err)
	}
	return nil
}

func (s *Server) writeInstallationTuples(ctx context.Context, app store.App, organizationID uuid.UUID) error {
	if len(app.Permissions) == 0 {
		return nil
	}
	tuples := make([]*authorizationv1.TupleKey, 0, len(app.Permissions))
	for _, permission := range app.Permissions {
		relation, ok := permissionToRelation[permission]
		if !ok {
			return status.Errorf(codes.Internal, "unknown permission %q", permission)
		}
		tuples = append(tuples, &authorizationv1.TupleKey{
			User:     fmt.Sprintf("identity:%s", app.IdentityID.String()),
			Relation: relation,
			Object:   fmt.Sprintf("organization:%s", organizationID.String()),
		})
	}
	if _, err := s.authorizationClient.Write(ctx, &authorizationv1.WriteRequest{Writes: tuples}); err != nil {
		return status.Errorf(codes.Internal, "authorization write: %v", err)
	}
	return nil
}

func (s *Server) deleteInstallationTuples(ctx context.Context, app store.App, organizationID uuid.UUID) {
	if len(app.Permissions) == 0 {
		return
	}
	tuples := make([]*authorizationv1.TupleKey, 0, len(app.Permissions))
	for _, permission := range app.Permissions {
		relation, ok := permissionToRelation[permission]
		if !ok {
			log.Printf("ERROR: unknown permission %q for installation cleanup", permission)
			continue
		}
		tuples = append(tuples, &authorizationv1.TupleKey{
			User:     fmt.Sprintf("identity:%s", app.IdentityID.String()),
			Relation: relation,
			Object:   fmt.Sprintf("organization:%s", organizationID.String()),
		})
	}
	if len(tuples) == 0 {
		return
	}
	if _, err := s.authorizationClient.Write(ctx, &authorizationv1.WriteRequest{Deletes: tuples}); err != nil {
		log.Printf("WARN: best-effort cleanup of installation tuples for org %s failed: %v", organizationID, err)
	}
}

func (s *Server) cleanupAuthorization(ctx context.Context, identityID uuid.UUID) {
	if _, err := s.authorizationClient.Write(ctx, &authorizationv1.WriteRequest{
		Deletes: []*authorizationv1.TupleKey{
			{
				User:     fmt.Sprintf("identity:%s", identityID.String()),
				Relation: clusterAppWriterRelation,
				Object:   clusterObject,
			},
		},
	}); err != nil {
		log.Printf("WARN: best-effort cleanup of authz tuple for identity %s failed: %v", identityID, err)
	}
}

func (s *Server) cleanupZitiIdentity(ctx context.Context, identityID uuid.UUID, zitiServiceID string) {
	if err := s.deleteZitiIdentity(ctx, identityID, zitiServiceID); err != nil {
		log.Printf("WARN: best-effort cleanup of ziti identity %s failed: %v", identityID, err)
	}
}

func (s *Server) deleteZitiIdentity(ctx context.Context, identityID uuid.UUID, zitiServiceID string) error {
	_, err := s.zitiManagementClient.DeleteAppIdentity(ctx, &zitimanagementv1.DeleteAppIdentityRequest{
		IdentityId:    identityID.String(),
		ZitiServiceId: zitiServiceID,
	})
	return err
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
	return token, hashServiceToken(token), nil
}

func hashServiceToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
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
