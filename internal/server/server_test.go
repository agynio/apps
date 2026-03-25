package server

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	authorizationv1 "github.com/agynio/apps/.gen/go/agynio/api/authorization/v1"
	identityv1 "github.com/agynio/apps/.gen/go/agynio/api/identity/v1"
	zitimanagementv1 "github.com/agynio/apps/.gen/go/agynio/api/ziti_management/v1"
	storepkg "github.com/agynio/apps/internal/store"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeStore struct {
	createFn               func(ctx context.Context, input storepkg.CreateAppInput) (storepkg.App, error)
	getFn                  func(ctx context.Context, id uuid.UUID) (storepkg.App, error)
	getBySlugFn            func(ctx context.Context, slug string) (storepkg.App, error)
	getByIdentityFn        func(ctx context.Context, id uuid.UUID) (storepkg.App, error)
	getByServiceTokenFn    func(ctx context.Context, tokenHash string) (storepkg.App, error)
	listFn                 func(ctx context.Context, pageSize int, pageToken string) ([]storepkg.App, string, error)
	deleteFn               func(ctx context.Context, id uuid.UUID) error
	createInputs           []storepkg.CreateAppInput
	getCalls               []uuid.UUID
	getByServiceTokenCalls []string
}

func (f *fakeStore) CreateApp(ctx context.Context, input storepkg.CreateAppInput) (storepkg.App, error) {
	f.createInputs = append(f.createInputs, input)
	if f.createFn != nil {
		return f.createFn(ctx, input)
	}
	return storepkg.App{}, errors.New("create app not implemented")
}

func (f *fakeStore) GetApp(ctx context.Context, id uuid.UUID) (storepkg.App, error) {
	f.getCalls = append(f.getCalls, id)
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return storepkg.App{}, errors.New("get app not implemented")
}

func (f *fakeStore) GetAppBySlug(ctx context.Context, slug string) (storepkg.App, error) {
	if f.getBySlugFn != nil {
		return f.getBySlugFn(ctx, slug)
	}
	return storepkg.App{}, errors.New("get app by slug not implemented")
}

func (f *fakeStore) GetAppByIdentityID(ctx context.Context, id uuid.UUID) (storepkg.App, error) {
	if f.getByIdentityFn != nil {
		return f.getByIdentityFn(ctx, id)
	}
	return storepkg.App{}, errors.New("get app by identity not implemented")
}

func (f *fakeStore) GetAppByServiceTokenHash(ctx context.Context, tokenHash string) (storepkg.App, error) {
	f.getByServiceTokenCalls = append(f.getByServiceTokenCalls, tokenHash)
	if f.getByServiceTokenFn != nil {
		return f.getByServiceTokenFn(ctx, tokenHash)
	}
	return storepkg.App{}, errors.New("get app by service token not implemented")
}

func (f *fakeStore) ListApps(ctx context.Context, pageSize int, pageToken string) ([]storepkg.App, string, error) {
	if f.listFn != nil {
		return f.listFn(ctx, pageSize, pageToken)
	}
	return nil, "", errors.New("list apps not implemented")
}

func (f *fakeStore) DeleteApp(ctx context.Context, id uuid.UUID) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return errors.New("delete app not implemented")
}

type fakeIdentityClient struct {
	registerFn       func(ctx context.Context, req *identityv1.RegisterIdentityRequest) (*identityv1.RegisterIdentityResponse, error)
	registerRequests []*identityv1.RegisterIdentityRequest
}

func (f *fakeIdentityClient) RegisterIdentity(ctx context.Context, req *identityv1.RegisterIdentityRequest, _ ...grpc.CallOption) (*identityv1.RegisterIdentityResponse, error) {
	f.registerRequests = append(f.registerRequests, req)
	if f.registerFn != nil {
		return f.registerFn(ctx, req)
	}
	return &identityv1.RegisterIdentityResponse{}, nil
}

func (f *fakeIdentityClient) GetIdentityType(ctx context.Context, _ *identityv1.GetIdentityTypeRequest, _ ...grpc.CallOption) (*identityv1.GetIdentityTypeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeIdentityClient) BatchGetIdentityTypes(ctx context.Context, _ *identityv1.BatchGetIdentityTypesRequest, _ ...grpc.CallOption) (*identityv1.BatchGetIdentityTypesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

type fakeAuthorizationClient struct {
	checkFn       func(ctx context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error)
	writeFn       func(ctx context.Context, req *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error)
	checkRequests []*authorizationv1.CheckRequest
	writeRequests []*authorizationv1.WriteRequest
}

func (f *fakeAuthorizationClient) Check(ctx context.Context, req *authorizationv1.CheckRequest, _ ...grpc.CallOption) (*authorizationv1.CheckResponse, error) {
	f.checkRequests = append(f.checkRequests, req)
	if f.checkFn != nil {
		return f.checkFn(ctx, req)
	}
	return &authorizationv1.CheckResponse{Allowed: true}, nil
}

func (f *fakeAuthorizationClient) BatchCheck(ctx context.Context, _ *authorizationv1.BatchCheckRequest, _ ...grpc.CallOption) (*authorizationv1.BatchCheckResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeAuthorizationClient) Write(ctx context.Context, req *authorizationv1.WriteRequest, _ ...grpc.CallOption) (*authorizationv1.WriteResponse, error) {
	f.writeRequests = append(f.writeRequests, req)
	if f.writeFn != nil {
		return f.writeFn(ctx, req)
	}
	return &authorizationv1.WriteResponse{}, nil
}

func (f *fakeAuthorizationClient) Read(ctx context.Context, _ *authorizationv1.ReadRequest, _ ...grpc.CallOption) (*authorizationv1.ReadResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeAuthorizationClient) ListObjects(ctx context.Context, _ *authorizationv1.ListObjectsRequest, _ ...grpc.CallOption) (*authorizationv1.ListObjectsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeAuthorizationClient) ListUsers(ctx context.Context, _ *authorizationv1.ListUsersRequest, _ ...grpc.CallOption) (*authorizationv1.ListUsersResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

type fakeZitiManagementClient struct {
	createFn       func(ctx context.Context, req *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error)
	deleteFn       func(ctx context.Context, req *zitimanagementv1.DeleteAppIdentityRequest) (*zitimanagementv1.DeleteAppIdentityResponse, error)
	createRequests []*zitimanagementv1.CreateAppIdentityRequest
	deleteRequests []*zitimanagementv1.DeleteAppIdentityRequest
}

func (f *fakeZitiManagementClient) CreateAgentIdentity(ctx context.Context, _ *zitimanagementv1.CreateAgentIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateAgentIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) CreateAppIdentity(ctx context.Context, req *zitimanagementv1.CreateAppIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateAppIdentityResponse, error) {
	f.createRequests = append(f.createRequests, req)
	if f.createFn != nil {
		return f.createFn(ctx, req)
	}
	return &zitimanagementv1.CreateAppIdentityResponse{ZitiIdentityId: "ziti-id", ZitiServiceId: "ziti-service"}, nil
}

func (f *fakeZitiManagementClient) DeleteIdentity(ctx context.Context, _ *zitimanagementv1.DeleteIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) DeleteAppIdentity(ctx context.Context, req *zitimanagementv1.DeleteAppIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteAppIdentityResponse, error) {
	f.deleteRequests = append(f.deleteRequests, req)
	if f.deleteFn != nil {
		return f.deleteFn(ctx, req)
	}
	return &zitimanagementv1.DeleteAppIdentityResponse{}, nil
}

func (f *fakeZitiManagementClient) ListManagedIdentities(ctx context.Context, _ *zitimanagementv1.ListManagedIdentitiesRequest, _ ...grpc.CallOption) (*zitimanagementv1.ListManagedIdentitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) ResolveIdentity(ctx context.Context, _ *zitimanagementv1.ResolveIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.ResolveIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) RequestServiceIdentity(ctx context.Context, _ *zitimanagementv1.RequestServiceIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.RequestServiceIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) ExtendIdentityLease(ctx context.Context, _ *zitimanagementv1.ExtendIdentityLeaseRequest, _ ...grpc.CallOption) (*zitimanagementv1.ExtendIdentityLeaseResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func newAdminContext() (context.Context, uuid.UUID) {
	callerID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, callerID.String()))
	return ctx, callerID
}

func TestRegisterAppSuccess(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}

	store := &fakeStore{}
	store.createFn = func(_ context.Context, input storepkg.CreateAppInput) (storepkg.App, error) {
		return storepkg.App{
			Meta: storepkg.EntityMeta{
				ID:        input.ID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Slug:             input.Slug,
			Name:             input.Name,
			Description:      input.Description,
			Icon:             input.Icon,
			IdentityID:       input.IdentityID,
			ServiceTokenHash: input.ServiceTokenHash,
			ZitiIdentityID:   input.ZitiIdentityID,
			ZitiServiceID:    input.ZitiServiceID,
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.RegisterApp(ctx, &appsv1.RegisterAppRequest{
		Slug:        "demo",
		Name:        "Demo",
		Description: "A demo app",
		Icon:        "icon.png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetServiceToken() == "" {
		t.Fatalf("expected service token")
	}
	if len(store.createInputs) != 1 {
		t.Fatalf("expected create app to be called")
	}
	if store.createInputs[0].ServiceTokenHash != hashServiceToken(resp.GetServiceToken()) {
		t.Fatalf("service token hash did not match")
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected one identity registration call")
	}
	if identityClient.registerRequests[0].IdentityType != identityv1.IdentityType_IDENTITY_TYPE_APP {
		t.Fatalf("expected identity type app")
	}
	if len(authorizationClient.writeRequests) != 1 || len(authorizationClient.writeRequests[0].Writes) != 1 {
		t.Fatalf("expected authorization write")
	}
	if len(zitiClient.deleteRequests) != 0 {
		t.Fatalf("did not expect ziti delete")
	}
}

func TestRegisterAppRollbackOnZitiError(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	zitiClient.createFn = func(_ context.Context, _ *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error) {
		return nil, errors.New("ziti down")
	}
	store := &fakeStore{}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.RegisterApp(ctx, &appsv1.RegisterAppRequest{Slug: "demo", Name: "Demo"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected identity registration before ziti failure")
	}
	if len(authorizationClient.writeRequests) != 0 {
		t.Fatalf("did not expect authz write")
	}
	if len(store.createInputs) != 0 {
		t.Fatalf("did not expect store create")
	}
}

func TestRegisterAppRollbackOnAuthzWriteError(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	authorizationClient.writeFn = func(_ context.Context, _ *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error) {
		return nil, status.Error(codes.Internal, "authz down")
	}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.RegisterApp(ctx, &appsv1.RegisterAppRequest{Slug: "demo", Name: "Demo"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti cleanup")
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected identity registration before authz failure")
	}
	if len(store.createInputs) != 0 {
		t.Fatalf("did not expect store create")
	}
}

func TestRegisterAppRollbackOnStoreError(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}
	store.createFn = func(_ context.Context, _ storepkg.CreateAppInput) (storepkg.App, error) {
		return storepkg.App{}, storepkg.AlreadyExists("app")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.RegisterApp(ctx, &appsv1.RegisterAppRequest{Slug: "demo", Name: "Demo"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected already exists, got %v", status.Code(err))
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti cleanup")
	}
	if len(authorizationClient.writeRequests) != 2 {
		t.Fatalf("expected authz cleanup")
	}
	if len(authorizationClient.writeRequests[1].Deletes) != 1 {
		t.Fatalf("expected authz delete in cleanup")
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected identity registration before store failure")
	}
}

func TestDeleteApp(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	appID := uuid.New()
	identityID := uuid.New()

	store := &fakeStore{}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			IdentityID:     identityID,
			ZitiIdentityID: "ziti-id",
			ZitiServiceID:  "ziti-service",
		}, nil
	}
	store.deleteFn = func(_ context.Context, _ uuid.UUID) error { return nil }

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.DeleteApp(ctx, &appsv1.DeleteAppRequest{Id: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti delete")
	}
	if len(authorizationClient.writeRequests) != 1 || len(authorizationClient.writeRequests[0].Deletes) != 1 {
		t.Fatalf("expected authz delete")
	}
}

func TestValidateServiceTokenHashesServerSide(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, tokenHash string) (storepkg.App, error) {
		if tokenHash != hashServiceToken("raw-token") {
			return storepkg.App{}, errors.New("unexpected token hash")
		}
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ValidateServiceToken(context.Background(), &appsv1.ValidateServiceTokenRequest{TokenHash: "raw-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetApp().GetMeta().GetId() != appID.String() {
		t.Fatalf("expected app id %s, got %s", appID.String(), resp.GetApp().GetMeta().GetId())
	}
}
