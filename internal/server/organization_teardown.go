package server

import (
	"context"

	appsv1 "github.com/agynio/apps/.gen/go/agynio/api/apps/v1"
	"github.com/agynio/apps/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// teardownPageSize drains the paginated list endpoints a page at a time. The
// listing shrinks as the loop deletes, so each pass re-reads from the start
// rather than carrying a cursor across a mutating walk.
const teardownPageSize = 100

// DeleteOrganizationResources removes the organization's app installations, the
// apps it published, and those apps' installations in *other* organizations. It
// is internal: Istio settles who may call it, so there is no permission check
// and no caller identity to check against. Step 3 of the organization teardown,
// after the agents those installations could reach.
//
// This is the one step that reaches outside the organization. Publishing an app
// is a commitment that outlives the publisher's own use of it, and deleting the
// publisher ends it -- so a public app's installations elsewhere are uninstalled
// too. The organizations that installed them get no notice; that is a product
// question this does not answer.
//
// Idempotent by construction: a retried step lists nothing and deletes nothing.
func (s *Server) DeleteOrganizationResources(ctx context.Context, req *appsv1.DeleteOrganizationResourcesRequest) (*appsv1.DeleteOrganizationResourcesResponse, error) {
	organizationID, err := parseUUID(req.GetOrganizationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "organization_id: %v", err)
	}

	// Installations into this organization, whoever published the app.
	if err := s.uninstallAll(ctx, store.ListInstallationsFilter{OrganizationID: &organizationID}); err != nil {
		return nil, err
	}

	apps, err := s.listAllApps(ctx, store.ListAppsFilter{OrganizationID: &organizationID})
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		appID := app.Meta.ID
		// Installations of this app anywhere, including other organizations.
		// DeleteApp refuses while any remain, and that invariant is honoured
		// rather than worked around.
		if err := s.uninstallAll(ctx, store.ListInstallationsFilter{AppID: &appID}); err != nil {
			return nil, err
		}
		if app.ZitiServiceID != "" {
			if err := s.deleteZitiIdentity(ctx, app.IdentityID, app.ZitiServiceID); err != nil {
				return nil, status.Errorf(codes.Internal, "delete ziti identity: %v", err)
			}
		}
		if err := s.store.DeleteApp(ctx, appID); err != nil {
			return nil, toStatusError(err)
		}
	}
	return &appsv1.DeleteOrganizationResourcesResponse{}, nil
}

// uninstallAll removes every installation matching the filter, taking the
// installation's tuples off with each. It re-lists from the first page after
// every batch because the deletes shrink the collection under a cursor.
func (s *Server) uninstallAll(ctx context.Context, filter store.ListInstallationsFilter) error {
	for {
		installations, _, err := s.store.ListInstallations(ctx, teardownPageSize, "", filter)
		if err != nil {
			return toStatusError(err)
		}
		if len(installations) == 0 {
			return nil
		}
		for _, installation := range installations {
			app, err := s.store.GetApp(ctx, installation.AppID)
			if err != nil {
				return toStatusError(err)
			}
			s.deleteInstallationTuples(ctx, app, installation.OrganizationID)
			if err := s.store.DeleteInstallation(ctx, installation.Meta.ID); err != nil {
				return toStatusError(err)
			}
		}
	}
}

func (s *Server) listAllApps(ctx context.Context, filter store.ListAppsFilter) ([]store.App, error) {
	apps := []store.App{}
	pageToken := ""
	for {
		page, next, err := s.store.ListApps(ctx, teardownPageSize, pageToken, filter)
		if err != nil {
			return nil, toStatusError(err)
		}
		apps = append(apps, page...)
		if next == "" {
			return apps, nil
		}
		pageToken = next
	}
}
