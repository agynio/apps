package server

import (
	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	"github.com/agynio/apps/internal/store"
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
	}
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
