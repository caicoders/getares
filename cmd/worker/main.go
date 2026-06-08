package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"

	workerv1 "github.com/idevcm/Getares/gen/worker/v1"
	"github.com/idevcm/Getares/internal/worker"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// TODO: Issue #3 + #4 + #6 — start gRPC server, llama-server subprocess,
// and registration/heartbeat loop.
// Filled in during Sprint 1, issues #3, #4, and #6.

func main() {
	var nodeID, listenAddr, modelID string

	root := &cobra.Command{
		Use: "worker",
		Short: "Getares worker node",
		RunE: func (cmd *cobra.Command, _ []string) error {
			lis, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", listenAddr, err)
			}
			srv := grpc.NewServer()
			workerv1.RegisterWorkerServiceServer(srv, worker.NewServer(nodeID, modelID))
			slog.Info("worker listening", "addr", listenAddr)
			return srv.Serve(lis)
		},
	}
	root.Flags().StringVar(&nodeID, "id", "worker-1", "Node identifier")
	root.Flags().StringVar(&listenAddr, "listen", ":9091", "gRPC listen address")
	root.Flags().StringVar(&modelID, "model-id", "default", "Model advertised to coordinator")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
