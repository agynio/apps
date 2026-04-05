package server

import (
	"fmt"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	"github.com/agynio/apps/internal/store"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoEntityMeta(meta store.EntityMeta) *appsv1.EntityMeta {
	return &appsv1.EntityMeta{
		Id:        meta.ID.String(),
		CreatedAt: timestamppb.New(meta.CreatedAt),
		UpdatedAt: timestamppb.New(meta.UpdatedAt),
	}
}

func toProtoApp(app store.App) *appsv1.App {
	return &appsv1.App{
		Meta:           toProtoEntityMeta(app.Meta),
		Slug:           app.Slug,
		Name:           app.Name,
		Description:    app.Description,
		Icon:           app.Icon,
		IdentityId:     app.IdentityID.String(),
		ZitiIdentityId: app.ZitiIdentityID,
		ZitiServiceId:  app.ZitiServiceID,
		OrganizationId: app.OrganizationID.String(),
		Visibility:     toProtoVisibility(app.Visibility),
		Permissions:    app.Permissions,
	}
}

func toProtoInstallation(installation store.Installation) (*appsv1.Installation, error) {
	configuration, err := mapToProtoStruct(installation.Configuration)
	if err != nil {
		return nil, err
	}
	return &appsv1.Installation{
		Meta:           toProtoEntityMeta(installation.Meta),
		AppId:          installation.AppID.String(),
		OrganizationId: installation.OrganizationID.String(),
		Slug:           installation.Slug,
		Configuration:  configuration,
	}, nil
}

func toProtoVisibility(visibility store.AppVisibility) appsv1.AppVisibility {
	switch visibility {
	case store.AppVisibilityPublic:
		return appsv1.AppVisibility_APP_VISIBILITY_PUBLIC
	case store.AppVisibilityInternal:
		return appsv1.AppVisibility_APP_VISIBILITY_INTERNAL
	default:
		panic("unknown visibility")
	}
}

func toStoreVisibility(visibility appsv1.AppVisibility) (store.AppVisibility, error) {
	switch visibility {
	case appsv1.AppVisibility_APP_VISIBILITY_PUBLIC:
		return store.AppVisibilityPublic, nil
	case appsv1.AppVisibility_APP_VISIBILITY_INTERNAL:
		return store.AppVisibilityInternal, nil
	default:
		return "", fmt.Errorf("unknown visibility %v", visibility)
	}
}

func protoStructToMap(value *structpb.Struct) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value.AsMap()
}

func mapToProtoStruct(value map[string]any) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}
	result, err := structpb.NewStruct(value)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func toProtoAppProfile(app store.App) *appsv1.AppProfile {
	return &appsv1.AppProfile{
		Id:          app.Meta.ID.String(),
		Slug:        app.Slug,
		Name:        app.Name,
		Description: app.Description,
		Icon:        app.Icon,
	}
}
