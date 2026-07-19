// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	coordinatorv1 "github.com/caicoders/getares/gen/coordinator/v1"
	"github.com/caicoders/getares/internal/api/openai"
	"github.com/caicoders/getares/internal/coordinator"
)

func main() {
    var grpcAddr, httpAddr string

    root := &cobra.Command{
        Use:   "coordinator",
        Short: "Getares coordinator node",
        RunE: func(cmd *cobra.Command, _ []string) error {

            // ── 1. Create the Registry ──────────────────────────────────────
            // NewRegistry() internally starts the eviction goroutine.
            // From this point on, the registry clears dead workers on its own.
            reg := coordinator.NewRegistry()

            // ── 2. gRPC server (for workers) ──────────────────────────
            grpcLis, err := net.Listen("tcp", grpcAddr)
            if err != nil {
                return err
            }

            grpcSrv := grpc.NewServer()
            coordinatorv1.RegisterCoordinatorServiceServer(
                grpcSrv,
                coordinator.NewServer(reg),
            )

            // The gRPC server blocks—we run it in a goroutine
            // so that the main goroutine can continue and start the HTTP server.
            slog.Info("gRPC coordinator listening", "addr", grpcAddr)
            go func() {
                if err := grpcSrv.Serve(grpcLis); err != nil {
                    slog.Error("The gRPC server has stopped", "err", err)
                    os.Exit(1)
                }
            }()

            // ── 3. HTTP Server (for clients) ─────────────────────────
            handler := openai.NewHandler(reg)

            // ListenAndServe blocks — it runs in the main goroutine.
            // If the HTTP server fails, the process terminates with an error.
            slog.Info("HTTP coordinator listening", "addr", httpAddr)
            return http.ListenAndServe(httpAddr, handler)
        },
    }

    root.Flags().StringVar(&grpcAddr, "grpc", ":9090", "gRPC Endpoint for Workers")
    root.Flags().StringVar(&httpAddr, "http", ":8080", "HTTP Address for Clients")

    if err := root.Execute(); err != nil {
        os.Exit(1)
    }
}
