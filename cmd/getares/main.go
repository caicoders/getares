// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	coordinatorv1 "github.com/caicoders/getares/gen/coordinator/v1"
	workerv1 "github.com/caicoders/getares/gen/worker/v1"
	openaiapi "github.com/caicoders/getares/internal/api/openai"
	"github.com/caicoders/getares/internal/config"
	"github.com/caicoders/getares/internal/coordinator"
	"github.com/caicoders/getares/internal/wizard"
	"github.com/caicoders/getares/internal/worker"
)

func main() {
	root := &cobra.Command{
		Use:   "getares",
		Short: "Getares — Distributed AI Runtime for LAN inference",
		Long: `Getares is a lightweight distributed AI runtime.
It routes AI inference requests across machines on your local network,
using the hardware you already have.`,
	}

	root.AddCommand(
		initCmd(),
		startCmd(),
		coordinatorCmd(),
		workerCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── getares init ──────────────────────────────────────────────────────────────

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard — generates getares.yaml",
		Long: `Detects your hardware, suggests compatible AI models,
and generates a getares.yaml configuration file.
Run 'getares start' afterwards.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wizard.Run()
		},
	}
}

// ── getares start ─────────────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Getares using getares.yaml",
		Long:  "Reads getares.yaml and starts the coordinator, worker, or both.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			slog.Info("starting Getares", "role", cfg.Role, "config", config.DefaultConfigFile)

			switch cfg.Role {
			case config.RoleCoordinator:
				return startCoordinator(cfg.Coordinator.GRPCAddr, cfg.Coordinator.HTTPAddr)

			case config.RoleWorker:
				return startWorker(
					cfg.NodeID,
					cfg.Worker.Listen,
					cfg.Worker.LlamaPort,
					cfg.Worker.ModelPath,
					cfg.Worker.ModelID,
					cfg.Worker.Coordinator,
				)

			case config.RoleBoth:
				// Start coordinator in goroutine, worker in main goroutine
				coordErrors := make(chan error, 1)
				go func() {
					coordErrors <- startCoordinator(
						cfg.Coordinator.GRPCAddr,
						cfg.Coordinator.HTTPAddr,
					)
				}()

				// Give coordinator a moment to bind ports before worker tries to connect
				time.Sleep(500 * time.Millisecond)

				// Check coordinator didn't fail immediately
				select {
				case err := <-coordErrors:
					return fmt.Errorf("coordinator failed to start: %w", err)
				default:
				}

				return startWorker(
					cfg.NodeID,
					cfg.Worker.Listen,
					cfg.Worker.LlamaPort,
					cfg.Worker.ModelPath,
					cfg.Worker.ModelID,
					cfg.Worker.Coordinator,
				)

			default:
				return fmt.Errorf("unknown role %q in getares.yaml — valid values: coordinator, worker, both", cfg.Role)
			}
		},
	}
}

// ── getares coordinator ───────────────────────────────────────────────────────

func coordinatorCmd() *cobra.Command {
	var grpcAddr, httpAddr string

	cmd := &cobra.Command{
		Use:   "coordinator",
		Short: "Start the coordinator node (manual flags)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return startCoordinator(grpcAddr, httpAddr)
		},
	}

	cmd.Flags().StringVar(&grpcAddr, "grpc", ":9090", "gRPC address for worker registration")
	cmd.Flags().StringVar(&httpAddr, "http", ":8080", "HTTP address for the OpenAI-compatible API")
	return cmd
}

// ── getares worker ────────────────────────────────────────────────────────────

func workerCmd() *cobra.Command {
	var nodeID, listenAddr, modelPath, modelID, coordAddr string
	var llamaPort int

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start a worker node (manual flags)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return startWorker(nodeID, listenAddr, llamaPort, modelPath, modelID, coordAddr)
		},
	}

	cmd.Flags().StringVar(&nodeID,     "id",          "worker-1",       "Unique node identifier")
	cmd.Flags().StringVar(&listenAddr, "listen",      ":9091",          "gRPC listen address")
	cmd.Flags().IntVar(&llamaPort,     "llama-port",  8081,             "llama-server HTTP port")
	cmd.Flags().StringVar(&modelPath,  "model",       "",               "Path to the GGUF model file")
	cmd.Flags().StringVar(&modelID,    "model-id",    "default",        "Model alias (e.g. phi3, llama3)")
	cmd.Flags().StringVar(&coordAddr,  "coordinator", "localhost:9090", "Coordinator gRPC address")
	cmd.MarkFlagRequired("model")
	return cmd
}

// ── Shared start functions ────────────────────────────────────────────────────

func startCoordinator(grpcAddr, httpAddr string) error {
	reg := coordinator.NewRegistry()

	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", grpcAddr, err)
	}

	grpcSrv := grpc.NewServer()
	coordinatorv1.RegisterCoordinatorServiceServer(grpcSrv, coordinator.NewServer(reg))

	slog.Info("coordinator gRPC listening", "addr", grpcAddr)
	go func() {
		if err := grpcSrv.Serve(grpcLis); err != nil {
			slog.Error("gRPC server stopped", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("coordinator HTTP listening", "addr", httpAddr)
	return http.ListenAndServe(httpAddr, openaiapi.NewHandler(reg))
}

func startWorker(nodeID, listenAddr string, llamaPort int, modelPath, modelID, coordAddr string) error {
	llama, err := worker.StartLlamaServer(modelPath, llamaPort)
	if err != nil {
		return fmt.Errorf("llama-server: %w", err)
	}
	defer llama.Stop()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}

	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, worker.NewServer(nodeID, modelID, llama))
	reflection.Register(grpcSrv)

	slog.Info("worker gRPC listening", "addr", listenAddr, "model", modelID)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			slog.Error("worker gRPC stopped", "err", err)
			os.Exit(1)
		}
	}()

	_, portStr, _ := net.SplitHostPort(listenAddr)
	port, _ := strconv.Atoi(portStr)
	return registerLoop(nodeID, port, modelID, coordAddr)
}

// ── Registration + heartbeat (same logic as cmd/worker/main.go) ───────────────

func registerLoop(nodeID string, port int, modelID, coordAddr string) error {
	localIP := outboundIP()

	for {
		conn, err := grpc.NewClient(coordAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("dial coordinator: %w", err)
		}

		client := coordinatorv1.NewCoordinatorServiceClient(conn)

		slog.Info("registering with coordinator",
			"coordinator", coordAddr,
			"worker", fmt.Sprintf("%s:%d", localIP, port),
		)

		resp, err := client.Register(context.Background(), &coordinatorv1.RegisterRequest{
			NodeId:   nodeID,
			Address:  localIP,
			Port:     int32(port),
			ModelIds: []string{modelID},
		})
		if err != nil {
			slog.Warn("coordinator unavailable, retrying in 5s", "err", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		if !resp.Accepted {
			conn.Close()
			return fmt.Errorf("coordinator rejected registration: %s", resp.Message)
		}

		slog.Info("registered successfully", "ip", localIP, "port", port, "model", modelID)

		if err := heartbeatLoop(client, nodeID); err != nil {
			slog.Warn("heartbeat failed, re-registering", "err", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		conn.Close()
		return nil
	}
}

func heartbeatLoop(client coordinatorv1.CoordinatorServiceClient, nodeID string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		_, err := client.Heartbeat(context.Background(), &coordinatorv1.HeartbeatRequest{
			NodeId: nodeID,
		})
		if err != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
	}
	return nil
}

func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
