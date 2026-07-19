// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	coordinatorv1 "github.com/caicoders/getares/gen/coordinator/v1"
	workerv1 "github.com/caicoders/getares/gen/worker/v1"
	"github.com/caicoders/getares/internal/worker"
)

func main() {
	var (
		nodeID     string
		listenAddr string
		llamaPort  int
		modelPath  string
		modelID    string
		coordAddr  string
	)

	root := &cobra.Command{
		Use:   "worker",
		Short: "Getares worker node",
		RunE: func(cmd *cobra.Command, _ []string) error {

			// STEP A: Start llama-server
			llama, err := worker.StartLlamaServer(modelPath, llamaPort)
			if err != nil {
				return fmt.Errorf("llama-server: %w", err)
			}
			defer llama.Stop()

			// STEP B: Start the gRPC server
			lis, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", listenAddr, err)
			}

			grpcSrv := grpc.NewServer()
			workerv1.RegisterWorkerServiceServer(
				grpcSrv,
				worker.NewServer(nodeID, modelID, llama),
			)
			reflection.Register(grpcSrv)

			slog.Info("worker listening", "addr", listenAddr, "model", modelID)

			// STEP C: gRPC server in a goroutine — does not block the main function
			go func() {
				if err := grpcSrv.Serve(lis); err != nil {
					slog.Error("The gRPC server terminated unexpectedly", "err", err)
					os.Exit(1)
				}
			}()

			// STEP D: Extract the port number from the --listen flag
            // net.SplitHostPort splits “0.0.0.0:9091” into (“0.0.0.0”, “9091”)
			_, portStr, err := net.SplitHostPort(listenAddr)
			if err != nil {
				return fmt.Errorf("Invalid listening address: %w", err)
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return fmt.Errorf("invalid port: %w", err)
			}

			// STEP E: Register and maintain heartbeats — block indefinitely
			return registerLoop(nodeID, port, modelID, coordAddr)
		},
	}

	root.Flags().StringVar(&nodeID, "id", "worker-1", "Unique node identifier")
	root.Flags().StringVar(&listenAddr, "listen", ":9091", "The gRPC address where the worker listens")
	root.Flags().IntVar(&llamaPort, "llama-port", 8081, "Local port for the HTTP server")
	root.Flags().StringVar(&modelPath, "model", "", "Full path to the model's GGUF file")
	root.Flags().StringVar(&modelID, "model-id", "default", "Model alias (e.g., phi3)")
	root.Flags().StringVar(&coordAddr, "coordinator", "localhost:9090", "The coordinator's gRPC endpoint")

	root.MarkFlagRequired("model")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── Logging and Heartbeat Functions ────────────────────────────────────────

func registerLoop(nodeID string, port int, modelID, coordAddr string) error {
	localIP := outboundIP()

	for {
		conn, err := grpc.NewClient(
			coordAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return fmt.Errorf("Unable to create a gRPC client for the coordinator: %w", err)
		}

		client := coordinatorv1.NewCoordinatorServiceClient(conn)

		slog.Info("trying to register",
			"coordinator", coordAddr,
			"worker_addr", fmt.Sprintf("%s:%d", localIP, port),
		)

		resp, err := client.Register(context.Background(), &coordinatorv1.RegisterRequest{
			NodeId:   nodeID,
			Address:  localIP,
			Port:     int32(port),
			ModelIds: []string{modelID},
		})
		if err != nil {
			slog.Warn("Coordinator unavailable, retrying in 5 seconds", "err", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		if !resp.Accepted {
			conn.Close()
			return fmt.Errorf("The coordinator rejected the registration: %s", resp.Message)
		}

		slog.Info("registered successfully",
			"ip", localIP,
			"port", port,
			"model", modelID,
		)

		err = heartbeatLoop(conn, client, nodeID)
		conn.Close()
		if err != nil {
			slog.Warn("Heartbeat failed; retrying registration", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		return nil
	}
}

func heartbeatLoop(conn *grpc.ClientConn, client coordinatorv1.CoordinatorServiceClient, nodeID string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	slog.Info("heartbeat loop started", "interval", "5s")

	for range ticker.C {
		_, err := client.Heartbeat(context.Background(), &coordinatorv1.HeartbeatRequest{
			NodeId: nodeID,
		})
		if err != nil {
			return fmt.Errorf("heartbeat failed: %w", err)
		}
	}

	return nil
}

// outboundIP returns this machine's LAN IP address using the UDP dial trick.
func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
