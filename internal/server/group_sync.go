package server

import (
	"context"
	"fmt"
	"sort"

	groupsv1 "github.com/agynio/apps/.gen/go/agynio/api/groups/v1"
	zitimanagementv1 "github.com/agynio/apps/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/apps/internal/store"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

const (
	groupMembershipAddedSubject   = "agyn.groups.membership.added"
	groupMembershipRemovedSubject = "agyn.groups.membership.removed"
	groupRoleAttributePrefix      = "group-"
)

func (s *Server) appGroupRoleAttributes(ctx context.Context, app store.App) ([]string, error) {
	if s.groupsClient == nil {
		return []string{}, nil
	}
	groups, err := s.listAppGroups(ctx, app)
	if err != nil {
		return nil, err
	}
	roleAttributes := make([]string, 0, len(groups))
	seen := map[string]struct{}{}
	for _, group := range groups {
		groupID := group.GetMeta().GetId()
		if groupID == "" {
			return nil, fmt.Errorf("groups list returned group without id")
		}
		roleAttribute := groupRoleAttribute(groupID)
		if _, ok := seen[roleAttribute]; ok {
			continue
		}
		seen[roleAttribute] = struct{}{}
		roleAttributes = append(roleAttributes, roleAttribute)
	}
	sort.Strings(roleAttributes)
	return roleAttributes, nil
}

func (s *Server) listAppGroups(ctx context.Context, app store.App) ([]*groupsv1.Group, error) {
	groups := []*groupsv1.Group{}
	pageToken := ""
	for {
		response, err := s.groupsClient.ListMemberGroups(ctx, &groupsv1.ListMemberGroupsRequest{
			MemberType:     groupsv1.GroupMemberType_GROUP_MEMBER_TYPE_APP,
			MemberId:       app.IdentityID.String(),
			OrganizationId: app.OrganizationID.String(),
			PageSize:       int32(store.MaxListPageSize),
			PageToken:      pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list member groups: %w", err)
		}
		groups = append(groups, response.GetGroups()...)
		pageToken = response.GetNextPageToken()
		if pageToken == "" {
			return groups, nil
		}
	}
}

func (s *Server) HandleGroupMembershipEvent(ctx context.Context, subject string, data []byte) error {
	switch subject {
	case groupMembershipAddedSubject:
		event := &groupsv1.GroupMembershipAddedEvent{}
		if err := proto.Unmarshal(data, event); err != nil {
			return fmt.Errorf("unmarshal group membership added event: %w", err)
		}
		return s.handleAppMembershipChange(ctx, event.GetMemberType(), event.GetMemberId(), event.GetGroupId())
	case groupMembershipRemovedSubject:
		event := &groupsv1.GroupMembershipRemovedEvent{}
		if err := proto.Unmarshal(data, event); err != nil {
			return fmt.Errorf("unmarshal group membership removed event: %w", err)
		}
		return s.handleAppMembershipChange(ctx, event.GetMemberType(), event.GetMemberId(), event.GetGroupId())
	default:
		return nil
	}
}

func (s *Server) handleAppMembershipChange(ctx context.Context, memberType groupsv1.GroupMemberType, memberID string, groupID string) error {
	if memberType != groupsv1.GroupMemberType_GROUP_MEMBER_TYPE_APP {
		return nil
	}
	appID, err := uuid.Parse(memberID)
	if err != nil {
		return fmt.Errorf("parse group membership member id: %w", err)
	}
	candidateRemoveAttributes := []string{}
	if groupID != "" {
		candidateRemoveAttributes = append(candidateRemoveAttributes, groupRoleAttribute(groupID))
	}
	return s.syncAppGroupRoles(ctx, appID, candidateRemoveAttributes)
}

func (s *Server) ReconcileAllAppGroupRoles(ctx context.Context) error {
	pageToken := ""
	for {
		apps, nextToken, err := s.store.ListApps(ctx, store.MaxListPageSize, pageToken, store.ListAppsFilter{})
		if err != nil {
			return fmt.Errorf("list apps: %w", err)
		}
		for _, app := range apps {
			if err := s.patchAppToCurrentGroupRoles(ctx, app, nil); err != nil {
				return err
			}
		}
		if nextToken == "" {
			return nil
		}
		pageToken = nextToken
	}
}

func (s *Server) syncAppGroupRoles(ctx context.Context, appID uuid.UUID, candidateRemoveAttributes []string) error {
	app, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	return s.patchAppToCurrentGroupRoles(ctx, app, candidateRemoveAttributes)
}

func (s *Server) patchAppToCurrentGroupRoles(ctx context.Context, app store.App, candidateRemoveAttributes []string) error {
	if app.ZitiIdentityID == "" {
		return nil
	}
	desiredAttributes, err := s.appGroupRoleAttributes(ctx, app)
	if err != nil {
		return err
	}
	_, err = s.zitiManagementClient.PatchIdentityRoleAttributes(ctx, &zitimanagementv1.PatchIdentityRoleAttributesRequest{
		ZitiIdentityId: app.ZitiIdentityID,
		Add:            desiredAttributes,
		Remove:         staleCandidateAttributes(desiredAttributes, candidateRemoveAttributes),
	})
	if err != nil {
		return fmt.Errorf("patch app role attributes: %w", err)
	}
	return nil
}

func staleCandidateAttributes(desiredAttributes []string, candidateAttributes []string) []string {
	desired := make(map[string]struct{}, len(desiredAttributes))
	for _, attr := range desiredAttributes {
		desired[attr] = struct{}{}
	}
	remove := []string{}
	seen := map[string]struct{}{}
	for _, attr := range candidateAttributes {
		if attr == "" {
			continue
		}
		if _, ok := desired[attr]; ok {
			continue
		}
		if _, ok := seen[attr]; ok {
			continue
		}
		seen[attr] = struct{}{}
		remove = append(remove, attr)
	}
	sort.Strings(remove)
	return remove
}

func groupRoleAttribute(groupID string) string {
	return groupRoleAttributePrefix + groupID
}
