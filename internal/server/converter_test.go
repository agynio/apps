package server

import (
	"testing"
	"time"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	"github.com/agynio/apps/internal/store"
	"github.com/google/uuid"
)

func TestToProtoApp(t *testing.T) {
	appID := uuid.New()
	identityID := uuid.New()
	organizationID := uuid.New()
	createdAt := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Hour)
	app := store.App{
		Meta: store.EntityMeta{
			ID:        appID,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		},
		Slug:           "demo",
		Name:           "Demo",
		Description:    "A demo app",
		Icon:           "icon.png",
		IdentityID:     identityID,
		ZitiIdentityID: "ziti-id",
		ZitiServiceID:  "ziti-service",
		OrganizationID: organizationID,
		Visibility:     store.AppVisibilityInternal,
		Permissions:    []string{"thread:create"},
	}

	proto := toProtoApp(app)
	if proto.GetMeta().GetId() != appID.String() {
		t.Fatalf("expected meta id %s, got %s", appID.String(), proto.GetMeta().GetId())
	}
	if !proto.GetMeta().GetCreatedAt().AsTime().Equal(createdAt) {
		t.Fatalf("expected created_at %v, got %v", createdAt, proto.GetMeta().GetCreatedAt().AsTime())
	}
	if !proto.GetMeta().GetUpdatedAt().AsTime().Equal(updatedAt) {
		t.Fatalf("expected updated_at %v, got %v", updatedAt, proto.GetMeta().GetUpdatedAt().AsTime())
	}
	if proto.GetSlug() != app.Slug || proto.GetName() != app.Name || proto.GetDescription() != app.Description || proto.GetIcon() != app.Icon {
		t.Fatalf("proto fields did not match app")
	}
	if proto.GetIdentityId() != identityID.String() {
		t.Fatalf("expected identity id %s, got %s", identityID.String(), proto.GetIdentityId())
	}
	if proto.GetOrganizationId() != organizationID.String() {
		t.Fatalf("expected organization id %s, got %s", organizationID.String(), proto.GetOrganizationId())
	}
	if proto.GetVisibility() != appsv1.AppVisibility_APP_VISIBILITY_INTERNAL {
		t.Fatalf("expected internal visibility")
	}
	if len(proto.GetPermissions()) != 1 || proto.GetPermissions()[0] != "thread:create" {
		t.Fatalf("expected permissions to match")
	}
}

func TestToProtoAppProfile(t *testing.T) {
	appID := uuid.New()
	app := store.App{
		Meta: store.EntityMeta{ID: appID},
		Slug: "demo",
		Name: "Demo",
		Icon: "icon.png",
	}

	profile := toProtoAppProfile(app)
	if profile.GetId() != appID.String() {
		t.Fatalf("expected profile id %s, got %s", appID.String(), profile.GetId())
	}
	if profile.GetSlug() != app.Slug || profile.GetName() != app.Name || profile.GetIcon() != app.Icon {
		t.Fatalf("profile fields did not match app")
	}
}
