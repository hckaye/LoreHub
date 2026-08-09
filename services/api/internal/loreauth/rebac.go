package loreauth

import (
	"context"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RebacService implements the small companion service that Lore calls when it
// creates or removes a repository resource. It does not create an independent
// permission store: the PostgreSQL repository row remains the canonical
// partition record and every call is checked against the same token scope.
type RebacService struct {
	UnimplementedRebacApiServer
	auth *Service
}

func NewRebacService(auth *Service) (*RebacService, error) {
	if auth == nil {
		return nil, status.Error(codes.InvalidArgument, "Lore auth service is required")
	}
	return &RebacService{auth: auth}, nil
}

func (service *RebacService) CreateResource(
	ctx context.Context,
	request *CreateResourceRequest,
) (*CreateResourceResponse, error) {
	if request == nil || !authz.ValidResourceID(request.GetResourceId()) ||
		strings.TrimSpace(request.GetResourceName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "resource ID and name are required")
	}
	if len(request.GetResourceName()) > 256 {
		return nil, status.Error(codes.InvalidArgument, "resource name is too long")
	}
	claims, err := service.auth.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if !service.auth.tokenAllowsResource(claims, request.GetResourceId(), authz.PermissionAdmin) {
		return nil, status.Error(codes.PermissionDenied, "resource administration is outside the token scope")
	}
	if _, err := service.auth.policy.EffectivePermissions(ctx, claims.Subject, request.GetResourceId()); err != nil {
		return nil, status.Error(codes.NotFound, "resource is not registered in LoreHub")
	}
	return &CreateResourceResponse{}, nil
}

func (service *RebacService) DeleteResource(
	ctx context.Context,
	request *DeleteResourceRequest,
) (*DeleteResourceResponse, error) {
	if request == nil || !authz.ValidResourceID(request.GetResourceId()) {
		return nil, status.Error(codes.InvalidArgument, "resource ID is invalid")
	}
	claims, err := service.auth.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if !service.auth.tokenAllowsResource(claims, request.GetResourceId(), authz.PermissionAdmin) {
		return nil, status.Error(codes.PermissionDenied, "resource administration is outside the token scope")
	}
	if _, err := service.auth.policy.EffectivePermissions(ctx, claims.Subject, request.GetResourceId()); err != nil {
		return nil, status.Error(codes.NotFound, "resource is not registered in LoreHub")
	}
	return &DeleteResourceResponse{}, nil
}
