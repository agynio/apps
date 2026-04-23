package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeStore struct {
	createFn                          func(ctx context.Context, input storepkg.CreateAppInput) (storepkg.App, error)
	updateFn                          func(ctx context.Context, input storepkg.UpdateAppInput) (storepkg.App, error)
	getFn                             func(ctx context.Context, id uuid.UUID) (storepkg.App, error)
	getBySlugFn                       func(ctx context.Context, organizationID uuid.UUID, slug string) (storepkg.App, error)
	getByIdentityFn                   func(ctx context.Context, id uuid.UUID) (storepkg.App, error)
	getByServiceTokenFn               func(ctx context.Context, tokenHash string) (storepkg.App, error)
	listFn                            func(ctx context.Context, pageSize int, pageToken string, filter storepkg.ListAppsFilter) ([]storepkg.App, string, error)
	deleteFn                          func(ctx context.Context, id uuid.UUID) error
	hasActiveInstallationsFn          func(ctx context.Context, appID uuid.UUID) (bool, error)
	updateZitiIdentityFn              func(ctx context.Context, id uuid.UUID, zitiIdentityID string, zitiServiceID string) error
	createInstallationFn              func(ctx context.Context, input storepkg.CreateInstallationInput) (storepkg.Installation, error)
	getInstallationFn                 func(ctx context.Context, id uuid.UUID) (storepkg.Installation, error)
	getInstallationBySlugFn           func(ctx context.Context, organizationID uuid.UUID, slug string) (storepkg.Installation, error)
	listInstallationsFn               func(ctx context.Context, pageSize int, pageToken string, filter storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error)
	updateInstallationFn              func(ctx context.Context, input storepkg.UpdateInstallationInput) (storepkg.Installation, error)
	updateInstallationStatusFn        func(ctx context.Context, id uuid.UUID, status *string) (storepkg.Installation, error)
	deleteInstallationFn              func(ctx context.Context, id uuid.UUID) error
	appendInstallationAuditLogEntryFn func(ctx context.Context, input storepkg.AppendInstallationAuditLogEntryInput) (storepkg.InstallationAuditLogEntry, error)
	listInstallationAuditLogEntriesFn func(ctx context.Context, installationID uuid.UUID, pageSize int, pageToken string) ([]storepkg.InstallationAuditLogEntry, string, error)
	createInputs                      []storepkg.CreateAppInput
	updateInputs                      []storepkg.UpdateAppInput
	createInstallationInputs          []storepkg.CreateInstallationInput
	updateInstallationInputs          []storepkg.UpdateInstallationInput
	updateInstallationStatusCalls     []updateInstallationStatusCall
	appendInstallationAuditLogInputs  []storepkg.AppendInstallationAuditLogEntryInput
	listInstallationAuditLogCalls     []listAuditLogEntriesCall
	deleteInstallationCalls           []uuid.UUID
	deleteCalls                       []uuid.UUID
	getCalls                          []uuid.UUID
	getByServiceTokenCalls            []string
	updateZitiCalls                   []updateZitiCall
}

type updateZitiCall struct {
	id             uuid.UUID
	zitiIdentityID string
	zitiServiceID  string
}

type updateInstallationStatusCall struct {
	id     uuid.UUID
	status *string
}

type listAuditLogEntriesCall struct {
	installationID uuid.UUID
	pageSize       int
	pageToken      string
}

type auditLogStore struct {
	entries     []storepkg.InstallationAuditLogEntry
	idempotency map[string]storepkg.InstallationAuditLogEntry
	now         time.Time
}

func newAuditLogStore() *auditLogStore {
	return &auditLogStore{
		entries:     []storepkg.InstallationAuditLogEntry{},
		idempotency: map[string]storepkg.InstallationAuditLogEntry{},
		now:         time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
	}
}

func (s *auditLogStore) append(input storepkg.AppendInstallationAuditLogEntryInput) storepkg.InstallationAuditLogEntry {
	if input.IdempotencyKey != nil {
		if existing, ok := s.idempotency[*input.IdempotencyKey]; ok {
			return existing
		}
	}
	entry := storepkg.InstallationAuditLogEntry{
		ID:             uuid.New(),
		InstallationID: input.InstallationID,
		Message:        input.Message,
		Level:          input.Level,
		IdempotencyKey: input.IdempotencyKey,
		CreatedAt:      s.now,
	}
	s.now = s.now.Add(time.Minute)
	s.entries = append(s.entries, entry)
	if input.IdempotencyKey != nil {
		s.idempotency[*input.IdempotencyKey] = entry
	}
	if len(s.entries) > 1000 {
		removed := s.entries[0]
		s.entries = s.entries[1:]
		if removed.IdempotencyKey != nil {
			delete(s.idempotency, *removed.IdempotencyKey)
		}
	}
	return entry
}

func (s *auditLogStore) list(pageSize int, pageToken string) ([]storepkg.InstallationAuditLogEntry, string, error) {
	offset := 0
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil {
			return nil, "", err
		}
		offset = parsed
	}

	ordered := make([]storepkg.InstallationAuditLogEntry, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		ordered = append(ordered, s.entries[i])
	}

	if offset >= len(ordered) {
		return []storepkg.InstallationAuditLogEntry{}, "", nil
	}
	end := offset + pageSize
	if end > len(ordered) {
		end = len(ordered)
	}
	entries := ordered[offset:end]
	nextToken := ""
	if end < len(ordered) {
		nextToken = strconv.Itoa(end)
	}
	return entries, nextToken, nil
}

func (f *fakeStore) CreateApp(ctx context.Context, input storepkg.CreateAppInput) (storepkg.App, error) {
	f.createInputs = append(f.createInputs, input)
	if f.createFn != nil {
		return f.createFn(ctx, input)
	}
	return storepkg.App{}, errors.New("create app not implemented")
}

func (f *fakeStore) UpdateApp(ctx context.Context, input storepkg.UpdateAppInput) (storepkg.App, error) {
	f.updateInputs = append(f.updateInputs, input)
	if f.updateFn != nil {
		return f.updateFn(ctx, input)
	}
	return storepkg.App{}, errors.New("update app not implemented")
}

func (f *fakeStore) GetApp(ctx context.Context, id uuid.UUID) (storepkg.App, error) {
	f.getCalls = append(f.getCalls, id)
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return storepkg.App{}, errors.New("get app not implemented")
}

func (f *fakeStore) GetAppBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (storepkg.App, error) {
	if f.getBySlugFn != nil {
		return f.getBySlugFn(ctx, organizationID, slug)
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

func (f *fakeStore) ListApps(ctx context.Context, pageSize int, pageToken string, filter storepkg.ListAppsFilter) ([]storepkg.App, string, error) {
	if f.listFn != nil {
		return f.listFn(ctx, pageSize, pageToken, filter)
	}
	return nil, "", errors.New("list apps not implemented")
}

func (f *fakeStore) DeleteApp(ctx context.Context, id uuid.UUID) error {
	f.deleteCalls = append(f.deleteCalls, id)
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return errors.New("delete app not implemented")
}

func (f *fakeStore) HasActiveInstallations(ctx context.Context, appID uuid.UUID) (bool, error) {
	if f.hasActiveInstallationsFn != nil {
		return f.hasActiveInstallationsFn(ctx, appID)
	}
	return false, errors.New("has active installations not implemented")
}

func (f *fakeStore) UpdateAppZitiIdentity(ctx context.Context, id uuid.UUID, zitiIdentityID string, zitiServiceID string) error {
	f.updateZitiCalls = append(f.updateZitiCalls, updateZitiCall{
		id:             id,
		zitiIdentityID: zitiIdentityID,
		zitiServiceID:  zitiServiceID,
	})
	if f.updateZitiIdentityFn != nil {
		return f.updateZitiIdentityFn(ctx, id, zitiIdentityID, zitiServiceID)
	}
	return errors.New("update ziti identity not implemented")
}

func (f *fakeStore) CreateInstallation(ctx context.Context, input storepkg.CreateInstallationInput) (storepkg.Installation, error) {
	f.createInstallationInputs = append(f.createInstallationInputs, input)
	if f.createInstallationFn != nil {
		return f.createInstallationFn(ctx, input)
	}
	return storepkg.Installation{}, errors.New("create installation not implemented")
}

func (f *fakeStore) GetInstallation(ctx context.Context, id uuid.UUID) (storepkg.Installation, error) {
	if f.getInstallationFn != nil {
		return f.getInstallationFn(ctx, id)
	}
	return storepkg.Installation{}, errors.New("get installation not implemented")
}

func (f *fakeStore) GetInstallationBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (storepkg.Installation, error) {
	if f.getInstallationBySlugFn != nil {
		return f.getInstallationBySlugFn(ctx, organizationID, slug)
	}
	return storepkg.Installation{}, errors.New("get installation by slug not implemented")
}

func (f *fakeStore) ListInstallations(ctx context.Context, pageSize int, pageToken string, filter storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
	if f.listInstallationsFn != nil {
		return f.listInstallationsFn(ctx, pageSize, pageToken, filter)
	}
	return nil, "", errors.New("list installations not implemented")
}

func (f *fakeStore) UpdateInstallation(ctx context.Context, input storepkg.UpdateInstallationInput) (storepkg.Installation, error) {
	f.updateInstallationInputs = append(f.updateInstallationInputs, input)
	if f.updateInstallationFn != nil {
		return f.updateInstallationFn(ctx, input)
	}
	return storepkg.Installation{}, errors.New("update installation not implemented")
}

func (f *fakeStore) UpdateInstallationStatus(ctx context.Context, id uuid.UUID, status *string) (storepkg.Installation, error) {
	f.updateInstallationStatusCalls = append(f.updateInstallationStatusCalls, updateInstallationStatusCall{id: id, status: status})
	if f.updateInstallationStatusFn != nil {
		return f.updateInstallationStatusFn(ctx, id, status)
	}
	return storepkg.Installation{}, errors.New("update installation status not implemented")
}

func (f *fakeStore) AppendInstallationAuditLogEntry(ctx context.Context, input storepkg.AppendInstallationAuditLogEntryInput) (storepkg.InstallationAuditLogEntry, error) {
	f.appendInstallationAuditLogInputs = append(f.appendInstallationAuditLogInputs, input)
	if f.appendInstallationAuditLogEntryFn != nil {
		return f.appendInstallationAuditLogEntryFn(ctx, input)
	}
	return storepkg.InstallationAuditLogEntry{}, errors.New("append installation audit log entry not implemented")
}

func (f *fakeStore) ListInstallationAuditLogEntries(ctx context.Context, installationID uuid.UUID, pageSize int, pageToken string) ([]storepkg.InstallationAuditLogEntry, string, error) {
	f.listInstallationAuditLogCalls = append(f.listInstallationAuditLogCalls, listAuditLogEntriesCall{installationID: installationID, pageSize: pageSize, pageToken: pageToken})
	if f.listInstallationAuditLogEntriesFn != nil {
		return f.listInstallationAuditLogEntriesFn(ctx, installationID, pageSize, pageToken)
	}
	return nil, "", errors.New("list installation audit log entries not implemented")
}

func (f *fakeStore) DeleteInstallation(ctx context.Context, id uuid.UUID) error {
	f.deleteInstallationCalls = append(f.deleteInstallationCalls, id)
	if f.deleteInstallationFn != nil {
		return f.deleteInstallationFn(ctx, id)
	}
	return errors.New("delete installation not implemented")
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

func (f *fakeIdentityClient) SetNickname(ctx context.Context, _ *identityv1.SetNicknameRequest, _ ...grpc.CallOption) (*identityv1.SetNicknameResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeIdentityClient) RemoveNickname(ctx context.Context, _ *identityv1.RemoveNicknameRequest, _ ...grpc.CallOption) (*identityv1.RemoveNicknameResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeIdentityClient) ResolveNickname(ctx context.Context, _ *identityv1.ResolveNicknameRequest, _ ...grpc.CallOption) (*identityv1.ResolveNicknameResponse, error) {
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
	createFn              func(ctx context.Context, req *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error)
	createServiceFn       func(ctx context.Context, req *zitimanagementv1.CreateServiceRequest) (*zitimanagementv1.CreateServiceResponse, error)
	deleteFn              func(ctx context.Context, req *zitimanagementv1.DeleteAppIdentityRequest) (*zitimanagementv1.DeleteAppIdentityResponse, error)
	createRequests        []*zitimanagementv1.CreateAppIdentityRequest
	createServiceRequests []*zitimanagementv1.CreateServiceRequest
	deleteRequests        []*zitimanagementv1.DeleteAppIdentityRequest
}

func (f *fakeZitiManagementClient) CreateAgentIdentity(ctx context.Context, _ *zitimanagementv1.CreateAgentIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateAgentIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) CreateAppIdentity(ctx context.Context, req *zitimanagementv1.CreateAppIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateAppIdentityResponse, error) {
	f.createRequests = append(f.createRequests, req)
	if f.createFn != nil {
		return f.createFn(ctx, req)
	}
	return &zitimanagementv1.CreateAppIdentityResponse{ZitiIdentityId: "ziti-id", IdentityJson: []byte("identity-json")}, nil
}

func (f *fakeZitiManagementClient) CreateService(ctx context.Context, req *zitimanagementv1.CreateServiceRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateServiceResponse, error) {
	f.createServiceRequests = append(f.createServiceRequests, req)
	if f.createServiceFn != nil {
		return f.createServiceFn(ctx, req)
	}
	return &zitimanagementv1.CreateServiceResponse{ZitiServiceId: "ziti-service", ZitiServiceName: req.GetName()}, nil
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

func (f *fakeZitiManagementClient) CreateRunnerIdentity(ctx context.Context, _ *zitimanagementv1.CreateRunnerIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateRunnerIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) DeleteRunnerIdentity(ctx context.Context, _ *zitimanagementv1.DeleteRunnerIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteRunnerIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
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

func (f *fakeZitiManagementClient) CreateServicePolicy(ctx context.Context, _ *zitimanagementv1.CreateServicePolicyRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateServicePolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) DeleteServicePolicy(ctx context.Context, _ *zitimanagementv1.DeleteServicePolicyRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteServicePolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) DeleteService(ctx context.Context, _ *zitimanagementv1.DeleteServiceRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteServiceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) CreateDeviceIdentity(ctx context.Context, _ *zitimanagementv1.CreateDeviceIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.CreateDeviceIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (f *fakeZitiManagementClient) DeleteDeviceIdentity(ctx context.Context, _ *zitimanagementv1.DeleteDeviceIdentityRequest, _ ...grpc.CallOption) (*zitimanagementv1.DeleteDeviceIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func newAdminContext() (context.Context, uuid.UUID) {
	callerID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, callerID.String()))
	return ctx, callerID
}

func stringPtr(value string) *string {
	return &value
}

func TestCreateAppSuccess(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	zitiClient.createServiceFn = func(_ context.Context, req *zitimanagementv1.CreateServiceRequest) (*zitimanagementv1.CreateServiceResponse, error) {
		return &zitimanagementv1.CreateServiceResponse{ZitiServiceId: "service-id", ZitiServiceName: req.GetName()}, nil
	}
	organizationID := uuid.New()
	permissions := []string{"thread:create"}

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
			OrganizationID:   input.OrganizationID,
			Visibility:       input.Visibility,
			Permissions:      input.Permissions,
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.CreateApp(ctx, &appsv1.CreateAppRequest{
		OrganizationId: organizationID.String(),
		Slug:           "demo",
		Name:           "Demo",
		Description:    "A demo app",
		Icon:           "icon.png",
		Visibility:     appsv1.AppVisibility_APP_VISIBILITY_INTERNAL,
		Permissions:    permissions,
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
	if store.createInputs[0].OrganizationID != organizationID {
		t.Fatalf("expected organization id %s", organizationID)
	}
	if store.createInputs[0].Visibility != storepkg.AppVisibilityInternal {
		t.Fatalf("expected internal visibility")
	}
	if len(store.createInputs[0].Permissions) != 1 || store.createInputs[0].Permissions[0] != permissions[0] {
		t.Fatalf("expected permissions to be stored")
	}
	if store.createInputs[0].ServiceTokenHash != hashServiceToken(resp.GetServiceToken()) {
		t.Fatalf("service token hash did not match")
	}
	if store.createInputs[0].ZitiIdentityID != "" {
		t.Fatalf("expected empty ziti identity field")
	}
	if store.createInputs[0].ZitiServiceID != "service-id" {
		t.Fatalf("expected ziti service id to be stored")
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected organization ownership check")
	}
	if authorizationClient.checkRequests[0].GetTupleKey().GetRelation() != "owner" {
		t.Fatalf("expected owner relation check")
	}
	if authorizationClient.checkRequests[0].GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected organization check for %s", organizationID)
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected one identity registration call")
	}
	if identityClient.registerRequests[0].IdentityType != identityv1.IdentityType_IDENTITY_TYPE_APP {
		t.Fatalf("expected identity type app")
	}
	if len(authorizationClient.writeRequests) != 0 {
		t.Fatalf("did not expect authorization writes")
	}
	if len(zitiClient.createServiceRequests) != 1 {
		t.Fatalf("expected ziti service create")
	}
	if zitiClient.createServiceRequests[0].GetName() != "app-demo" {
		t.Fatalf("expected ziti service name to be app-demo")
	}
	if len(zitiClient.createServiceRequests[0].GetRoleAttributes()) != 1 || zitiClient.createServiceRequests[0].GetRoleAttributes()[0] != "app-services" {
		t.Fatalf("expected ziti service role attributes")
	}
	if len(zitiClient.createRequests) != 0 {
		t.Fatalf("did not expect ziti app identity create")
	}
	if len(zitiClient.deleteRequests) != 0 {
		t.Fatalf("did not expect ziti delete")
	}
}

func TestCreateAppRollbackOnStoreError(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	zitiClient.createServiceFn = func(_ context.Context, req *zitimanagementv1.CreateServiceRequest) (*zitimanagementv1.CreateServiceResponse, error) {
		return &zitimanagementv1.CreateServiceResponse{ZitiServiceId: "service-id", ZitiServiceName: req.GetName()}, nil
	}
	store := &fakeStore{}
	store.createFn = func(_ context.Context, _ storepkg.CreateAppInput) (storepkg.App, error) {
		return storepkg.App{}, storepkg.AlreadyExists("app")
	}
	organizationID := uuid.New()

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.CreateApp(ctx, &appsv1.CreateAppRequest{
		OrganizationId: organizationID.String(),
		Slug:           "demo",
		Name:           "Demo",
		Visibility:     appsv1.AppVisibility_APP_VISIBILITY_INTERNAL,
		Permissions:    []string{"thread:create"},
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected already exists, got %v", status.Code(err))
	}
	if len(authorizationClient.writeRequests) != 0 {
		t.Fatalf("did not expect authorization writes")
	}
	if len(identityClient.registerRequests) != 1 {
		t.Fatalf("expected identity registration before store failure")
	}
	if len(zitiClient.createServiceRequests) != 1 {
		t.Fatalf("expected ziti service create")
	}
	if len(zitiClient.createRequests) != 0 {
		t.Fatalf("did not expect ziti create")
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti cleanup")
	}
	if zitiClient.deleteRequests[0].GetIdentityId() != store.createInputs[0].IdentityID.String() {
		t.Fatalf("expected cleanup for identity %s", store.createInputs[0].IdentityID)
	}
	if zitiClient.deleteRequests[0].GetZitiServiceId() != "service-id" {
		t.Fatalf("expected cleanup for ziti service")
	}
}

func TestDeleteApp(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	appID := uuid.New()
	identityID := uuid.New()
	organizationID := uuid.New()

	store := &fakeStore{}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			IdentityID:     identityID,
			ZitiIdentityID: "ziti-id",
			ZitiServiceID:  "ziti-service",
			OrganizationID: organizationID,
		}, nil
	}
	store.hasActiveInstallationsFn = func(_ context.Context, _ uuid.UUID) (bool, error) { return false, nil }
	store.deleteFn = func(_ context.Context, _ uuid.UUID) error { return nil }

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.DeleteApp(ctx, &appsv1.DeleteAppRequest{Id: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti delete")
	}
	if zitiClient.deleteRequests[0].GetIdentityId() != identityID.String() {
		t.Fatalf("expected identity id %s", identityID)
	}
	if zitiClient.deleteRequests[0].GetZitiServiceId() != "ziti-service" {
		t.Fatalf("expected ziti service id cleanup")
	}
	if len(store.deleteCalls) != 1 {
		t.Fatalf("expected store delete")
	}
	if store.deleteCalls[0] != appID {
		t.Fatalf("expected store delete for %s", appID)
	}
}

func TestDeleteAppFailsWhenZitiCleanupFails(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	appID := uuid.New()
	identityID := uuid.New()
	organizationID := uuid.New()

	store := &fakeStore{}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			IdentityID:     identityID,
			ZitiIdentityID: "ziti-id",
			ZitiServiceID:  "ziti-service",
			OrganizationID: organizationID,
		}, nil
	}
	store.hasActiveInstallationsFn = func(_ context.Context, _ uuid.UUID) (bool, error) { return false, nil }
	store.deleteFn = func(_ context.Context, _ uuid.UUID) error { return nil }
	zitiClient.deleteFn = func(_ context.Context, _ *zitimanagementv1.DeleteAppIdentityRequest) (*zitimanagementv1.DeleteAppIdentityResponse, error) {
		return nil, errors.New("ziti cleanup failed")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.DeleteApp(ctx, &appsv1.DeleteAppRequest{Id: appID.String()})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti delete")
	}
	if len(store.deleteCalls) != 0 {
		t.Fatalf("expected store delete to be skipped")
	}
	if len(authorizationClient.writeRequests) != 0 {
		t.Fatalf("expected authz cleanup to be skipped")
	}
}

func TestDeleteAppAllowsMissingZitiIdentity(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	appID := uuid.New()
	identityID := uuid.New()
	organizationID := uuid.New()

	store := &fakeStore{}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			IdentityID:     identityID,
			ZitiIdentityID: "ziti-id",
			ZitiServiceID:  "ziti-service",
			OrganizationID: organizationID,
		}, nil
	}
	store.hasActiveInstallationsFn = func(_ context.Context, _ uuid.UUID) (bool, error) { return false, nil }
	store.deleteFn = func(_ context.Context, _ uuid.UUID) error { return nil }
	zitiClient.deleteFn = func(_ context.Context, _ *zitimanagementv1.DeleteAppIdentityRequest) (*zitimanagementv1.DeleteAppIdentityResponse, error) {
		return nil, status.Error(codes.NotFound, "not found")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.DeleteApp(ctx, &appsv1.DeleteAppRequest{Id: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti delete")
	}
	if len(store.deleteCalls) != 1 {
		t.Fatalf("expected store delete")
	}
}

func TestValidateServiceTokenHashesServerSide(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, tokenHash string) (storepkg.App, error) {
		if tokenHash != hashServiceToken("raw-token") {
			return storepkg.App{}, errors.New("unexpected token hash")
		}
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
		}, nil
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

func TestEnrollAppSuccess(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	identityID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, tokenHash string) (storepkg.App, error) {
		if tokenHash != hashServiceToken("raw-token") {
			return storepkg.App{}, errors.New("unexpected token hash")
		}
		return storepkg.App{
			Meta:          storepkg.EntityMeta{ID: appID},
			Slug:          "demo",
			IdentityID:    identityID,
			ZitiServiceID: "service-id",
		}, nil
	}
	zitiClient.createFn = func(_ context.Context, _ *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error) {
		return &zitimanagementv1.CreateAppIdentityResponse{
			ZitiIdentityId: "ziti-id",
			IdentityJson:   []byte("identity-json"),
		}, nil
	}
	store.updateZitiIdentityFn = func(_ context.Context, _ uuid.UUID, _ string, _ string) error { return nil }

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{ServiceToken: "raw-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.GetIdentityJson()) != "identity-json" {
		t.Fatalf("expected identity json payload")
	}
	if resp.GetIdentityId() != identityID.String() {
		t.Fatalf("expected identity id %s, got %s", identityID.String(), resp.GetIdentityId())
	}
	if len(zitiClient.createRequests) != 1 {
		t.Fatalf("expected ziti create")
	}
	if zitiClient.createRequests[0].GetIdentityId() != identityID.String() {
		t.Fatalf("expected identity id in ziti create request")
	}
	if zitiClient.createRequests[0].GetSlug() != "demo" {
		t.Fatalf("expected slug in ziti create request")
	}
	if len(store.updateZitiCalls) != 1 {
		t.Fatalf("expected ziti update call")
	}
	if store.updateZitiCalls[0].id != appID {
		t.Fatalf("expected update for app %s", appID)
	}
	if store.updateZitiCalls[0].zitiIdentityID != "ziti-id" || store.updateZitiCalls[0].zitiServiceID != "service-id" {
		t.Fatalf("unexpected ziti update values")
	}
	if len(zitiClient.deleteRequests) != 0 {
		t.Fatalf("did not expect ziti cleanup")
	}
}

func TestEnrollAppReenrollDoesNotCleanupExisting(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	identityID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, _ string) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			Slug:           "demo",
			IdentityID:     identityID,
			ZitiIdentityID: "old-ziti",
			ZitiServiceID:  "old-service",
		}, nil
	}
	zitiClient.createFn = func(_ context.Context, _ *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error) {
		return &zitimanagementv1.CreateAppIdentityResponse{ZitiIdentityId: "new-ziti"}, nil
	}
	store.updateZitiIdentityFn = func(_ context.Context, _ uuid.UUID, _ string, _ string) error { return nil }

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{ServiceToken: "raw-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.updateZitiCalls) != 1 {
		t.Fatalf("expected update call")
	}
	if store.updateZitiCalls[0].zitiServiceID != "old-service" {
		t.Fatalf("expected ziti service to come from app record")
	}
	if len(zitiClient.deleteRequests) != 0 {
		t.Fatalf("did not expect ziti cleanup before reenroll")
	}
}

func TestEnrollAppMissingToken(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", status.Code(err))
	}
}

func TestEnrollAppNotFound(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	store.getByServiceTokenFn = func(_ context.Context, _ string) (storepkg.App, error) {
		return storepkg.App{}, storepkg.NotFound("app")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{ServiceToken: "raw-token"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", status.Code(err))
	}
	if len(zitiClient.createRequests) != 0 {
		t.Fatalf("did not expect ziti create")
	}
	if len(store.updateZitiCalls) != 0 {
		t.Fatalf("did not expect update call")
	}
}

func TestEnrollAppCreateFailureDoesNotUpdate(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	identityID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, _ string) (storepkg.App, error) {
		return storepkg.App{
			Meta:          storepkg.EntityMeta{ID: appID},
			Slug:          "demo",
			IdentityID:    identityID,
			ZitiServiceID: "service-id",
		}, nil
	}
	zitiClient.createFn = func(_ context.Context, _ *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error) {
		return nil, errors.New("ziti down")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{ServiceToken: "raw-token"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(zitiClient.createRequests) != 1 {
		t.Fatalf("expected ziti create attempt")
	}
	if len(store.updateZitiCalls) != 0 {
		t.Fatalf("did not expect update call")
	}
	if len(zitiClient.deleteRequests) != 0 {
		t.Fatalf("did not expect ziti cleanup")
	}
}

func TestEnrollAppUpdateFailureCleansUpZiti(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	identityID := uuid.New()
	store.getByServiceTokenFn = func(_ context.Context, _ string) (storepkg.App, error) {
		return storepkg.App{
			Meta:          storepkg.EntityMeta{ID: appID},
			Slug:          "demo",
			IdentityID:    identityID,
			ZitiServiceID: "service-id",
		}, nil
	}
	zitiClient.createFn = func(_ context.Context, _ *zitimanagementv1.CreateAppIdentityRequest) (*zitimanagementv1.CreateAppIdentityResponse, error) {
		return &zitimanagementv1.CreateAppIdentityResponse{ZitiIdentityId: "ziti-id"}, nil
	}
	store.updateZitiIdentityFn = func(_ context.Context, _ uuid.UUID, _ string, _ string) error {
		return errors.New("db down")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.EnrollApp(context.Background(), &appsv1.EnrollAppRequest{ServiceToken: "raw-token"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(zitiClient.deleteRequests) != 1 {
		t.Fatalf("expected ziti cleanup after update failure")
	}
	if zitiClient.deleteRequests[0].GetIdentityId() != identityID.String() {
		t.Fatalf("expected cleanup for identity %s", identityID)
	}
	if zitiClient.deleteRequests[0].GetZitiServiceId() != "service-id" {
		t.Fatalf("expected cleanup for ziti service")
	}
}

func TestUpdateAppSuccess(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			OrganizationID: organizationID,
		}, nil
	}
	store.updateFn = func(_ context.Context, input storepkg.UpdateAppInput) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: input.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			Name:           *input.Name,
			Description:    *input.Description,
			Icon:           *input.Icon,
			Visibility:     *input.Visibility,
			OrganizationID: organizationID,
		}, nil
	}

	name := "Updated"
	description := "Updated description"
	icon := "updated.png"
	visibility := appsv1.AppVisibility_APP_VISIBILITY_PUBLIC

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.UpdateApp(ctx, &appsv1.UpdateAppRequest{
		Id:          appID.String(),
		Name:        &name,
		Description: &description,
		Icon:        &icon,
		Visibility:  &visibility,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.updateInputs) != 1 {
		t.Fatalf("expected update app input")
	}
	if store.updateInputs[0].Name == nil || *store.updateInputs[0].Name != name {
		t.Fatalf("expected name update")
	}
	if store.updateInputs[0].Description == nil || *store.updateInputs[0].Description != description {
		t.Fatalf("expected description update")
	}
	if store.updateInputs[0].Icon == nil || *store.updateInputs[0].Icon != icon {
		t.Fatalf("expected icon update")
	}
	if store.updateInputs[0].Visibility == nil || *store.updateInputs[0].Visibility != storepkg.AppVisibilityPublic {
		t.Fatalf("expected visibility update")
	}
	if resp.GetApp().GetName() != name {
		t.Fatalf("expected response name %s", name)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected org owner check")
	}
}

func TestGetAppPublicSkipsMemberCheck(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			Slug:           "public",
			Name:           "Public",
			OrganizationID: uuid.New(),
			Visibility:     storepkg.AppVisibilityPublic,
			IdentityID:     uuid.New(),
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetApp(ctx, &appsv1.GetAppRequest{Id: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 0 {
		t.Fatalf("did not expect authorization checks")
	}
}

func TestGetAppInternalRequiresMember(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			Slug:           "internal",
			Name:           "Internal",
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
			IdentityID:     uuid.New(),
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetApp(ctx, &appsv1.GetAppRequest{Id: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestGetAppBySlugInternalRequiresMember(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	store.getBySlugFn = func(_ context.Context, _ uuid.UUID, _ string) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			Slug:           "internal",
			Name:           "Internal",
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
			IdentityID:     uuid.New(),
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetAppBySlug(ctx, &appsv1.GetAppBySlugRequest{OrganizationId: organizationID.String(), Slug: "internal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestGetAppProfileRequiresAuth(t *testing.T) {
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetAppProfile(context.Background(), &appsv1.GetAppProfileRequest{IdentityId: uuid.New().String()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated, got %v", status.Code(err))
	}
}

func TestListAppsDefaultsToPublic(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	store.listFn = func(_ context.Context, _ int, _ string, filter storepkg.ListAppsFilter) ([]storepkg.App, string, error) {
		if filter.OrganizationID != nil {
			return nil, "", errors.New("expected no org filter")
		}
		if filter.Visibility == nil || *filter.Visibility != storepkg.AppVisibilityPublic {
			return nil, "", errors.New("expected public visibility")
		}
		return []storepkg.App{}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ListApps(ctx, &appsv1.ListAppsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetApps()) != 0 {
		t.Fatalf("expected no apps")
	}
	if len(authorizationClient.checkRequests) != 0 {
		t.Fatalf("did not expect authorization checks")
	}
}

func TestListAppsRejectsInternalWithoutOrg(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}
	called := false

	store.listFn = func(_ context.Context, _ int, _ string, _ storepkg.ListAppsFilter) ([]storepkg.App, string, error) {
		called = true
		return []storepkg.App{}, "", nil
	}

	visibility := appsv1.AppVisibility_APP_VISIBILITY_INTERNAL
	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ListApps(ctx, &appsv1.ListAppsRequest{Visibility: visibility})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
	if called {
		t.Fatalf("did not expect store list")
	}
}

func TestListAppsRequiresMemberForOrgFilter(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	organizationID := uuid.New()
	store.listFn = func(_ context.Context, _ int, _ string, filter storepkg.ListAppsFilter) ([]storepkg.App, string, error) {
		if filter.OrganizationID == nil || *filter.OrganizationID != organizationID {
			return nil, "", errors.New("expected org filter")
		}
		if filter.Visibility != nil {
			return nil, "", errors.New("expected no visibility filter")
		}
		return []storepkg.App{}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ListApps(ctx, &appsv1.ListAppsRequest{OrganizationId: organizationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestUpdateInstallationSuccess(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID},
			AppID:          appID,
			OrganizationID: organizationID,
		}, nil
	}
	store.updateInstallationFn = func(_ context.Context, input storepkg.UpdateInstallationInput) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: input.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          appID,
			OrganizationID: organizationID,
			Slug:           *input.Slug,
			Configuration:  *input.Configuration,
		}, nil
	}

	slug := "custom-install"
	configuration, err := structpb.NewStruct(map[string]any{"region": "us-east"})
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.UpdateInstallation(ctx, &appsv1.UpdateInstallationRequest{
		Id:            installationID.String(),
		Slug:          &slug,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.updateInstallationInputs) != 1 {
		t.Fatalf("expected update installation input")
	}
	if store.updateInstallationInputs[0].Slug == nil || *store.updateInstallationInputs[0].Slug != slug {
		t.Fatalf("expected slug update")
	}
	if store.updateInstallationInputs[0].Configuration == nil {
		t.Fatalf("expected configuration update")
	}
	if resp.GetInstallation().GetSlug() != slug {
		t.Fatalf("expected response slug %s", slug)
	}
	if resp.GetInstallation().GetConfiguration().AsMap()["region"] != "us-east" {
		t.Fatalf("expected response configuration")
	}
}

func TestGetInstallationRequiresMember(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          uuid.New(),
			OrganizationID: organizationID,
			Slug:           "install",
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetInstallation(ctx, &appsv1.GetInstallationRequest{Id: installationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestGetInstallationRejectsNonMember(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          uuid.New(),
			OrganizationID: organizationID,
			Slug:           "install",
		}, nil
	}
	authorizationClient.checkFn = func(_ context.Context, _ *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
		return &authorizationv1.CheckResponse{Allowed: false}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetInstallation(ctx, &appsv1.GetInstallationRequest{Id: installationID.String()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
}

func TestGetInstallationBySlugRequiresMember(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationBySlugFn = func(_ context.Context, _ uuid.UUID, _ string) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          uuid.New(),
			OrganizationID: organizationID,
			Slug:           "install",
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetInstallationBySlug(ctx, &appsv1.GetInstallationBySlugRequest{OrganizationId: organizationID.String(), Slug: "install"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestListInstallationsRequiresMemberForOrgFilter(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	organizationID := uuid.New()
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, filter storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		if filter.OrganizationID == nil || *filter.OrganizationID != organizationID {
			return nil, "", errors.New("expected org filter")
		}
		return []storepkg.Installation{}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{OrganizationId: organizationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected member check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestListInstallationsAllowsAppIdentityWithAppID(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	store.getByIdentityFn = func(_ context.Context, id uuid.UUID) (storepkg.App, error) {
		if id != identityID {
			return storepkg.App{}, fmt.Errorf("unexpected identity id")
		}
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: identityID}, nil
	}

	orgOne := uuid.New()
	orgTwo := uuid.New()
	now := time.Now()
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, filter storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		if filter.AppID == nil || *filter.AppID != appID {
			return nil, "", errors.New("expected app filter")
		}
		return []storepkg.Installation{
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
				AppID:          appID,
				OrganizationID: orgOne,
				Slug:           "one",
			},
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
				AppID:          appID,
				OrganizationID: orgTwo,
				Slug:           "two",
			},
		}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{AppId: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetInstallations()) != 2 {
		t.Fatalf("expected two installations")
	}
	if len(authorizationClient.checkRequests) != 0 {
		t.Fatalf("expected no member checks")
	}
}

func TestListInstallationsRejectsAppIdentityForOtherApp(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	otherAppID := uuid.New()
	store.getByIdentityFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: identityID}, nil
	}
	listCalled := false
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, _ storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		listCalled = true
		return []storepkg.Installation{}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{AppId: otherAppID.String()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
	if listCalled {
		t.Fatalf("did not expect list installations call")
	}
}

func TestListInstallationsFiltersByMembershipWithAppID(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	allowedOrg := uuid.New()
	blockedOrg := uuid.New()
	now := time.Now()
	store.getByIdentityFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{}, storepkg.NotFound("app")
	}
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, filter storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		if filter.AppID == nil || *filter.AppID != appID {
			return nil, "", errors.New("expected app filter")
		}
		return []storepkg.Installation{
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
				AppID:          appID,
				OrganizationID: allowedOrg,
				Slug:           "allowed",
			},
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
				AppID:          appID,
				OrganizationID: blockedOrg,
				Slug:           "blocked",
			},
		}, "", nil
	}
	authorizationClient.checkFn = func(_ context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
		if req.GetTupleKey().GetObject() == fmt.Sprintf("organization:%s", allowedOrg) {
			return &authorizationv1.CheckResponse{Allowed: true}, nil
		}
		return &authorizationv1.CheckResponse{Allowed: false}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{AppId: appID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetInstallations()) != 1 {
		t.Fatalf("expected one authorized installation")
	}
	if resp.GetInstallations()[0].GetOrganizationId() != allowedOrg.String() {
		t.Fatalf("expected installation for org %s", allowedOrg)
	}
	if len(authorizationClient.checkRequests) != 2 {
		t.Fatalf("expected membership checks for both orgs")
	}
	for _, check := range authorizationClient.checkRequests {
		if check.GetTupleKey().GetRelation() != "member" {
			t.Fatalf("expected member relation")
		}
		if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
			t.Fatalf("expected user to be %s", callerID)
		}
	}
}

func TestListInstallationsFiltersByMembership(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	allowedOrg := uuid.New()
	blockedOrg := uuid.New()
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, _ storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		return []storepkg.Installation{
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
				AppID:          uuid.New(),
				OrganizationID: allowedOrg,
				Slug:           "allowed",
			},
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
				AppID:          uuid.New(),
				OrganizationID: blockedOrg,
				Slug:           "blocked",
			},
		}, "", nil
	}
	authorizationClient.checkFn = func(_ context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
		if req.GetTupleKey().GetObject() == fmt.Sprintf("organization:%s", allowedOrg) {
			return &authorizationv1.CheckResponse{Allowed: true}, nil
		}
		return &authorizationv1.CheckResponse{Allowed: false}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetInstallations()) != 1 {
		t.Fatalf("expected one authorized installation")
	}
	if resp.GetInstallations()[0].GetOrganizationId() != allowedOrg.String() {
		t.Fatalf("expected installation for org %s", allowedOrg)
	}
	if len(authorizationClient.checkRequests) != 2 {
		t.Fatalf("expected membership checks for both orgs")
	}
	for _, check := range authorizationClient.checkRequests {
		if check.GetTupleKey().GetRelation() != "member" {
			t.Fatalf("expected member relation")
		}
		if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
			t.Fatalf("expected user to be %s", callerID)
		}
	}
}

func TestListInstallationsCachesMembershipChecks(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	organizationID := uuid.New()
	store.listInstallationsFn = func(_ context.Context, _ int, _ string, _ storepkg.ListInstallationsFilter) ([]storepkg.Installation, string, error) {
		return []storepkg.Installation{
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
				AppID:          uuid.New(),
				OrganizationID: organizationID,
				Slug:           "one",
			},
			{
				Meta:           storepkg.EntityMeta{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
				AppID:          uuid.New(),
				OrganizationID: organizationID,
				Slug:           "two",
			},
		}, "", nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ListInstallations(ctx, &appsv1.ListInstallationsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetInstallations()) != 2 {
		t.Fatalf("expected two installations")
	}
	if len(authorizationClient.checkRequests) != 1 {
		t.Fatalf("expected one membership check")
	}
	check := authorizationClient.checkRequests[0]
	if check.GetTupleKey().GetRelation() != "member" {
		t.Fatalf("expected member relation")
	}
	if check.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID) {
		t.Fatalf("expected user to be %s", callerID)
	}
	if check.GetTupleKey().GetObject() != fmt.Sprintf("organization:%s", organizationID) {
		t.Fatalf("expected org check for %s", organizationID)
	}
}

func TestInstallAppVisibilityEnforced(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	otherOrgID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			OrganizationID: otherOrgID,
			Visibility:     storepkg.AppVisibilityInternal,
		}, nil
	}
	store.createInstallationFn = func(_ context.Context, _ storepkg.CreateInstallationInput) (storepkg.Installation, error) {
		return storepkg.Installation{}, errors.New("unexpected create")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.InstallApp(ctx, &appsv1.InstallAppRequest{
		AppId:          appID.String(),
		OrganizationId: organizationID.String(),
		Slug:           "install",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
	if len(store.createInstallationInputs) != 0 {
		t.Fatalf("did not expect installation creation")
	}
}

func TestInstallAppDefaultsSlug(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			Slug:           "demo",
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
		}, nil
	}
	store.createInstallationFn = func(_ context.Context, input storepkg.CreateInstallationInput) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: input.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          input.AppID,
			OrganizationID: input.OrganizationID,
			Slug:           input.Slug,
			Configuration:  input.Configuration,
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.InstallApp(ctx, &appsv1.InstallAppRequest{
		AppId:          appID.String(),
		OrganizationId: organizationID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.createInstallationInputs) != 1 {
		t.Fatalf("expected installation creation")
	}
	if store.createInstallationInputs[0].Slug != "demo" {
		t.Fatalf("expected slug defaulted to app slug")
	}
	if resp.GetInstallation().GetSlug() != "demo" {
		t.Fatalf("expected response slug to be defaulted")
	}
}

func TestInstallAppWritesPermissionTuples(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	appIdentityID := uuid.New()
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			Slug:           "demo",
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
			IdentityID:     appIdentityID,
			Permissions:    []string{"thread:create", "thread:write", "participant:add"},
		}, nil
	}
	store.createInstallationFn = func(_ context.Context, input storepkg.CreateInstallationInput) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: input.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          input.AppID,
			OrganizationID: input.OrganizationID,
			Slug:           input.Slug,
			Configuration:  input.Configuration,
		}, nil
	}
	authorizationClient.writeFn = func(_ context.Context, req *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error) {
		if len(req.Writes) != 3 {
			return nil, errors.New("expected three permission tuples")
		}
		relations := map[string]bool{}
		for _, tuple := range req.Writes {
			if tuple.GetUser() != fmt.Sprintf("identity:%s", appIdentityID) {
				return nil, fmt.Errorf("unexpected user %s", tuple.GetUser())
			}
			if tuple.GetObject() != fmt.Sprintf("organization:%s", organizationID) {
				return nil, fmt.Errorf("unexpected object %s", tuple.GetObject())
			}
			relations[tuple.GetRelation()] = true
		}
		for _, relation := range []string{"thread_create", "thread_write", "participant_add"} {
			if !relations[relation] {
				return nil, fmt.Errorf("missing relation %s", relation)
			}
		}
		return &authorizationv1.WriteResponse{}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.InstallApp(ctx, &appsv1.InstallAppRequest{AppId: appID.String(), OrganizationId: organizationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(authorizationClient.writeRequests) != 1 {
		t.Fatalf("expected permission tuples to be written")
	}
}

func TestInstallAppRollbackOnTupleWriteError(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	appID := uuid.New()
	organizationID := uuid.New()
	installationID := uuid.New()
	order := make([]string, 0, 2)
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			Slug:           "demo",
			OrganizationID: organizationID,
			Visibility:     storepkg.AppVisibilityInternal,
			IdentityID:     uuid.New(),
			Permissions:    []string{"thread:create"},
		}, nil
	}
	store.createInstallationFn = func(_ context.Context, input storepkg.CreateInstallationInput) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			AppID:          input.AppID,
			OrganizationID: input.OrganizationID,
			Slug:           input.Slug,
			Configuration:  input.Configuration,
		}, nil
	}
	store.deleteInstallationFn = func(_ context.Context, _ uuid.UUID) error {
		order = append(order, "delete")
		return nil
	}
	authorizationClient.writeFn = func(_ context.Context, _ *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error) {
		order = append(order, "auth")
		return nil, status.Error(codes.Internal, "authz down")
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.InstallApp(ctx, &appsv1.InstallAppRequest{
		AppId:          appID.String(),
		OrganizationId: organizationID.String(),
		Slug:           "install",
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", status.Code(err))
	}
	if len(store.deleteInstallationCalls) != 1 {
		t.Fatalf("expected rollback delete")
	}
	if len(order) != 2 || order[0] != "auth" || order[1] != "delete" {
		t.Fatalf("expected auth then delete, got %v", order)
	}
}

func TestUninstallAppDeletesTuplesBeforeStore(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	organizationID := uuid.New()
	order := make([]string, 0, 2)
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID},
			AppID:          appID,
			OrganizationID: organizationID,
		}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:           storepkg.EntityMeta{ID: appID},
			IdentityID:     uuid.New(),
			OrganizationID: organizationID,
			Permissions:    []string{"thread:create"},
		}, nil
	}
	store.deleteInstallationFn = func(_ context.Context, _ uuid.UUID) error {
		order = append(order, "delete")
		return nil
	}
	authorizationClient.writeFn = func(_ context.Context, req *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error) {
		if len(req.Deletes) != 1 {
			return nil, errors.New("expected delete tuples")
		}
		if req.Deletes[0].GetRelation() != "thread_create" {
			return nil, errors.New("expected thread_create relation")
		}
		order = append(order, "auth")
		return &authorizationv1.WriteResponse{}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.UninstallApp(ctx, &appsv1.UninstallAppRequest{Id: installationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "auth" || order[1] != "delete" {
		t.Fatalf("expected auth then delete, got %v", order)
	}
}

func TestGetInstallationConfigurationAllowsAppIdentity(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:          storepkg.EntityMeta{ID: installationID},
			AppID:         appID,
			Configuration: map[string]any{"region": "eu"},
		}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:       storepkg.EntityMeta{ID: appID},
			IdentityID: identityID,
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.GetInstallationConfiguration(ctx, &appsv1.GetInstallationConfigurationRequest{Id: installationID.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetConfiguration().AsMap()["region"] != "eu" {
		t.Fatalf("expected configuration response")
	}
}

func TestGetInstallationConfigurationRejectsNonAppIdentity(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:  storepkg.EntityMeta{ID: installationID},
			AppID: appID,
		}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{
			Meta:       storepkg.EntityMeta{ID: appID},
			IdentityID: uuid.New(),
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.GetInstallationConfiguration(ctx, &appsv1.GetInstallationConfigurationRequest{Id: installationID.String()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
}

func TestReportInstallationStatusClearsWhitespace(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID},
			AppID:          appID,
			OrganizationID: organizationID,
		}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: identityID}, nil
	}
	store.updateInstallationStatusFn = func(_ context.Context, id uuid.UUID, status *string) (storepkg.Installation, error) {
		if id != installationID {
			return storepkg.Installation{}, fmt.Errorf("unexpected installation id")
		}
		if status != nil {
			return storepkg.Installation{}, fmt.Errorf("expected status cleared")
		}
		return storepkg.Installation{
			Meta:           storepkg.EntityMeta{ID: installationID},
			AppID:          appID,
			OrganizationID: organizationID,
		}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	resp, err := srv.ReportInstallationStatus(ctx, &appsv1.ReportInstallationStatusRequest{
		InstallationId: installationID.String(),
		Status:         "   ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetInstallation().GetStatus() != "" {
		t.Fatalf("expected cleared status")
	}
}

func TestReportInstallationStatusRejectsNonAppIdentity(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, AppID: appID}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: uuid.New()}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ReportInstallationStatus(ctx, &appsv1.ReportInstallationStatusRequest{
		InstallationId: installationID.String(),
		Status:         "ready",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
}

func TestAppendInstallationAuditLogEntryRejectsNonAppIdentity(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, AppID: appID}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: uuid.New()}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.AppendInstallationAuditLogEntry(ctx, &appsv1.AppendInstallationAuditLogEntryRequest{
		InstallationId: installationID.String(),
		Message:        "failed",
		Level:          appsv1.InstallationAuditLogLevel_INSTALLATION_AUDIT_LOG_LEVEL_ERROR,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
}

func TestAppendInstallationAuditLogEntryIdempotency(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	auditLog := newAuditLogStore()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, AppID: appID}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: identityID}, nil
	}
	store.appendInstallationAuditLogEntryFn = func(_ context.Context, input storepkg.AppendInstallationAuditLogEntryInput) (storepkg.InstallationAuditLogEntry, error) {
		return auditLog.append(input), nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	first, err := srv.AppendInstallationAuditLogEntry(ctx, &appsv1.AppendInstallationAuditLogEntryRequest{
		InstallationId: installationID.String(),
		Message:        "attempt",
		Level:          appsv1.InstallationAuditLogLevel_INSTALLATION_AUDIT_LOG_LEVEL_INFO,
		IdempotencyKey: stringPtr("same-key"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := srv.AppendInstallationAuditLogEntry(ctx, &appsv1.AppendInstallationAuditLogEntryRequest{
		InstallationId: installationID.String(),
		Message:        "attempt",
		Level:          appsv1.InstallationAuditLogLevel_INSTALLATION_AUDIT_LOG_LEVEL_INFO,
		IdempotencyKey: stringPtr("same-key"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.GetEntry().GetId() != second.GetEntry().GetId() {
		t.Fatalf("expected idempotent entry")
	}
	if len(auditLog.entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(auditLog.entries))
	}
}

func TestAppendInstallationAuditLogEntryRetention(t *testing.T) {
	identityID := uuid.New()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identityMetadata, identityID.String()))
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	auditLog := newAuditLogStore()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, AppID: appID}, nil
	}
	store.getFn = func(_ context.Context, _ uuid.UUID) (storepkg.App, error) {
		return storepkg.App{Meta: storepkg.EntityMeta{ID: appID}, IdentityID: identityID}, nil
	}
	store.appendInstallationAuditLogEntryFn = func(_ context.Context, input storepkg.AppendInstallationAuditLogEntryInput) (storepkg.InstallationAuditLogEntry, error) {
		return auditLog.append(input), nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	for i := 0; i < 1001; i++ {
		_, err := srv.AppendInstallationAuditLogEntry(ctx, &appsv1.AppendInstallationAuditLogEntryRequest{
			InstallationId: installationID.String(),
			Message:        fmt.Sprintf("entry-%d", i),
			Level:          appsv1.InstallationAuditLogLevel_INSTALLATION_AUDIT_LOG_LEVEL_INFO,
			IdempotencyKey: stringPtr(fmt.Sprintf("key-%d", i)),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(auditLog.entries) != 1000 {
		t.Fatalf("expected 1000 retained entries, got %d", len(auditLog.entries))
	}
	if auditLog.entries[0].Message != "entry-1" {
		t.Fatalf("expected oldest entry to be trimmed")
	}
}

func TestListInstallationAuditLogEntriesPagination(t *testing.T) {
	ctx, _ := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	appID := uuid.New()
	organizationID := uuid.New()
	auditLog := newAuditLogStore()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, AppID: appID, OrganizationID: organizationID}, nil
	}
	store.listInstallationAuditLogEntriesFn = func(_ context.Context, _ uuid.UUID, pageSize int, pageToken string) ([]storepkg.InstallationAuditLogEntry, string, error) {
		return auditLog.list(pageSize, pageToken)
	}

	for i := 0; i < 4; i++ {
		auditLog.append(storepkg.AppendInstallationAuditLogEntryInput{
			InstallationID: installationID,
			Message:        fmt.Sprintf("entry-%d", i),
			Level:          storepkg.InstallationAuditLogLevelInfo,
		})
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	first, err := srv.ListInstallationAuditLogEntries(ctx, &appsv1.ListInstallationAuditLogEntriesRequest{
		InstallationId: installationID.String(),
		PageSize:       2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(first.GetEntries()) != 2 {
		t.Fatalf("expected first page entries")
	}
	if first.GetEntries()[0].GetMessage() != "entry-3" || first.GetEntries()[1].GetMessage() != "entry-2" {
		t.Fatalf("unexpected page order")
	}
	if first.GetNextPageToken() == "" {
		t.Fatalf("expected next page token")
	}

	second, err := srv.ListInstallationAuditLogEntries(ctx, &appsv1.ListInstallationAuditLogEntriesRequest{
		InstallationId: installationID.String(),
		PageSize:       2,
		PageToken:      first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(second.GetEntries()) != 2 {
		t.Fatalf("expected second page entries")
	}
	if second.GetEntries()[0].GetMessage() != "entry-1" || second.GetEntries()[1].GetMessage() != "entry-0" {
		t.Fatalf("unexpected second page order")
	}
	if second.GetNextPageToken() != "" {
		t.Fatalf("expected no further page token")
	}
}

func TestListInstallationAuditLogEntriesRejectsNonMember(t *testing.T) {
	ctx, callerID := newAdminContext()
	identityClient := &fakeIdentityClient{}
	authorizationClient := &fakeAuthorizationClient{}
	zitiClient := &fakeZitiManagementClient{}
	store := &fakeStore{}

	installationID := uuid.New()
	organizationID := uuid.New()
	store.getInstallationFn = func(_ context.Context, _ uuid.UUID) (storepkg.Installation, error) {
		return storepkg.Installation{Meta: storepkg.EntityMeta{ID: installationID}, OrganizationID: organizationID}, nil
	}
	authorizationClient.checkFn = func(_ context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
		if req.GetTupleKey().GetUser() != fmt.Sprintf("identity:%s", callerID.String()) {
			return nil, errors.New("unexpected identity")
		}
		return &authorizationv1.CheckResponse{Allowed: false}, nil
	}

	srv := New(store, identityClient, authorizationClient, zitiClient)
	_, err := srv.ListInstallationAuditLogEntries(ctx, &appsv1.ListInstallationAuditLogEntriesRequest{
		InstallationId: installationID.String(),
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", status.Code(err))
	}
}
