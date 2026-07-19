// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package coordinator

import (
	"context"
	coordinatorv1 "github.com/caicoders/getares/gen/coordinator/v1"
)

type Server struct {
    coordinatorv1.UnimplementedCoordinatorServiceServer
    reg *Registry
}

func NewServer(reg *Registry) *Server { return &Server{reg: reg} }

func (s *Server) Register(_ context.Context, req *coordinatorv1.RegisterRequest) (*coordinatorv1.RegisterResponse, error) {
    if err := s.reg.Add(req.NodeId, req.Address, int(req.Port), req.ModelIds); err != nil {
        return &coordinatorv1.RegisterResponse{Accepted: false, Message: err.Error()}, nil
    }
    return &coordinatorv1.RegisterResponse{Accepted: true}, nil
}

func (s *Server) Heartbeat(_ context.Context, req *coordinatorv1.HeartbeatRequest) (*coordinatorv1.HeartbeatResponse, error) {
    s.reg.Touch(req.NodeId)
    return &coordinatorv1.HeartbeatResponse{Ok: true}, nil
}