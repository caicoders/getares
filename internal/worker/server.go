package worker

import (
	"context"
	workerv1 "github.com/idevcm/Getares/gen/worker/v1"
)

type Server struct {
	workerv1.UnimplementedWorkerServiceServer
	nodeID string
	modelID string
	llama *LlamaServer
}

func NewServer(nodeID, modelID string) *Server {
	return  &Server{nodeID: nodeID, modelID: modelID}
}

func(s *Server) Health(_ context.Context, _ *workerv1.HealthRequest) (*workerv1.HealthResponse, error) {
	return  &workerv1.HealthResponse{
		NodeId: s.nodeID,
		Status: "ready",
		LoadedModels: []string{s.modelID},
	}, nil
}

func (s *Server) Infer(req *workerv1.InferRequest, stream workerv1.WorkerService_InferServer) error {
	// Placeholder replaced in Issue #4
	return stream.Send(&workerv1.InferChunk{
		Token: "pong",
		Done: true,
		FinishReason: "stop",
	})
}

// We define the empty struct so that Go knows it exists.
// This will resolve the “undefined” error.
type LlamaServer struct {
    // Empty for now until Issue #4 is resolved
}