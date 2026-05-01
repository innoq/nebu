//go:build go1.22

// Package api contains the Admin API handler stubs generated from openapi.yaml.
// All operations return 501 Not Implemented until Epic 6 sub-stories wire real handlers.
package api

import "context"

// AdminServer implements StrictServerInterface.
// Every method returns 501 Not Implemented; Epic 6 sub-stories replace these one by one.
type AdminServer struct{}

// Ensure AdminServer satisfies the generated StrictServerInterface at compile time.
var _ StrictServerInterface = (*AdminServer)(nil)

func (s *AdminServer) GetAdminConfig(_ context.Context, _ GetAdminConfigRequestObject) (GetAdminConfigResponseObject, error) {
	return GetAdminConfig501Response{}, nil
}

func (s *AdminServer) GetAdminMetrics(_ context.Context, _ GetAdminMetricsRequestObject) (GetAdminMetricsResponseObject, error) {
	return GetAdminMetrics501Response{}, nil
}

func (s *AdminServer) ListAdminRooms(_ context.Context, _ ListAdminRoomsRequestObject) (ListAdminRoomsResponseObject, error) {
	return ListAdminRooms501Response{}, nil
}

func (s *AdminServer) ListAdminUsers(_ context.Context, _ ListAdminUsersRequestObject) (ListAdminUsersResponseObject, error) {
	return ListAdminUsers501Response{}, nil
}

func (s *AdminServer) ListComplianceAccessRequests(_ context.Context, _ ListComplianceAccessRequestsRequestObject) (ListComplianceAccessRequestsResponseObject, error) {
	return ListComplianceAccessRequests501Response{}, nil
}

func (s *AdminServer) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}
