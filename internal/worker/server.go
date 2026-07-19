// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package worker

import (
	"context"
	workerv1 "github.com/caicoders/getares/gen/worker/v1"
)

type Server struct {
	workerv1.UnimplementedWorkerServiceServer
	nodeID  string
	modelID string
	llama   *LlamaServer
}

func NewServer(nodeID, modelID string, llama *LlamaServer) *Server {
	return &Server{
		nodeID: nodeID, 
		modelID: modelID, 
		llama: llama,
	}
}

func (s *Server) Health(_ context.Context, _ *workerv1.HealthRequest) (*workerv1.HealthResponse, error) {
	return &workerv1.HealthResponse{
		NodeId:       s.nodeID,
		Status:       "ready",
		LoadedModels: []string{s.modelID},
	}, nil
}

// stream.Context() retrieves the context.Context from the gRPC stream. 
// This context is canceled if the client disconnects. We pass it to 
// llama.Infer() so that the cancellation chain works properly.
func (s *Server) Infer(req *workerv1.InferRequest, stream workerv1.WorkerService_InferServer) error {
    return s.llama.Infer(stream.Context(), req, stream)
}
