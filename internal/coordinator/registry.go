// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package coordinator

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	workerv1 "github.com/caicoders/getares/gen/worker/v1"
)

const nodeTTL = 15 * time.Second

// Node represents a worker registered in the cluster.
// It contains both the node's metadata and the gRPC connection
// that the coordinator uses to call Infer() and Health().
type Node struct {
    ID           string
    Address      string
    Port         int
    LoadedModels []string
    LastSeen     time.Time // actualizado en cada Heartbeat()

    // conn and Client are the actual gRPC connection to the worker.
    // They are created in Add() and closed in evict().
    // Java analogy: like a preconfigured HttpClient channel
    // with the worker's IP and port.
    conn   *grpc.ClientConn
    Client workerv1.WorkerServiceClient
}

// Registry is the central repository for active workers.
// All operations are thread-safe—multiple goroutines
// (one for each incoming RPC) can read and write concurrently.
type Registry struct {
    mu    sync.RWMutex      // like ReadWriteLock in Java
    nodes map[string]*Node  // nodeID → Node
}

// NewRegistry creates an empty registry and starts the
// background eviction goroutine.
//
// The eviction goroutine is like a @Scheduled annotation in Spring:
// it runs every 5 seconds indefinitely, without anyone calling it.
func NewRegistry() *Registry {
    r := &Registry{
        nodes: make(map[string]*Node),
    }
    go r.evict() // We launch the goroutine and forget about it—it runs on its own
    return r
}

// Add registers a new worker (or re-registers an existing one after
// a reconnection) and opens a gRPC connection to it.
//
// If the worker already existed (re-registration after the coordinator went down),
// we close the old connection before creating the new one.
func (r *Registry) Add(id, address string, port int, models []string) error {
    target := fmt.Sprintf("%s:%d", address, port)

    // We open the gRPC connection to the worker.
    // grpc.NewClient does NOT establish the connection immediately—it is lazy.
    // The actual connection is established on the first RPC call.
    // insecure.NewCredentials() = no TLS (we will add TLS in Sprint 4)
    conn, err := grpc.NewClient(
        target,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        return fmt.Errorf("Unable to create a gRPC client to %s: %w", target, err)
    }

    // Exclusive lock — we're going to write to the map
    // defer Unlock() ensures that the lock is always released,
    // even if there is an early return.
    r.mu.Lock()
    defer r.mu.Unlock()

    // If the worker was already registered, we close the old connection.
    // This happens when a worker reconnects after a restart.
    if old, ok := r.nodes[id]; ok {
        old.conn.Close()
    }

    r.nodes[id] = &Node{
        ID:           id,
        Address:      address,
        Port:         port,
        LoadedModels: models,
        LastSeen:     time.Now(),
        conn:         conn,
        Client:       workerv1.NewWorkerServiceClient(conn),
    }

    slog.Info("registered worker", "id", id, "addr", target, "models", models)
    return nil
}

// Touch updates a worker's LastSeen timestamp.
// It is called in every Heartbeat(). Without Touch(), the worker
// would be evicted even if it is still alive.
func (r *Registry) Touch(id string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    if n, ok := r.nodes[id]; ok {
        n.LastSeen = time.Now()
    }
    // If the ID doesn't exist, we silently ignore it.
    // This can happen if a heartbeat arrives right after an eviction.
}

// Pick returns a gRPC client to a worker that has
// the requested model loaded.
//
// Sprint 1: Returns the first one it finds.
// Sprint 2: This is where the scorer comes in, with VRAM, active sessions, etc.
func (r *Registry) Pick(modelID string) (workerv1.WorkerServiceClient, error) {
    // RLock — read-only; multiple goroutines can read at the same time.
    // If we used Lock() here, we would block all requests
    // while selecting a worker. With RLock, requests are concurrent.
    r.mu.RLock()
    defer r.mu.RUnlock()

    // Preference 1: a worker that already has the model loaded
    for _, n := range r.nodes {
        for _, m := range n.LoadedModels {
            if m == modelID {
                return n.Client, nil
            }
        }
    }

    // Preference 2: any available worker (fallback)
    // In Sprint 3, this fallback will trigger LoadModel() on the selected worker
    for _, n := range r.nodes {
        return n.Client, nil
    }

    return nil, fmt.Errorf("There are no workers available for the model %q", modelID)
}

// evict runs in the background indefinitely.
// Every 5 seconds, it checks all workers and removes those that
// have not sent a heartbeat in the last 15 seconds (nodeTTL).
//
// Java analogy: like a @Scheduled(fixedRate = 5000) that clears
// expired entries from a cache.
func (r *Registry) evict() {
    // time.Tick returns a channel that receives the current time every 5 seconds.
    // The `for range` loop on that channel executes the body every 5 seconds,
    // blocking between executions. The goroutine never terminates.
    for range time.Tick(5 * time.Second) {
        r.mu.Lock()
        for id, n := range r.nodes {
            if time.Since(n.LastSeen) > nodeTTL {
                n.conn.Close()           // Close the gRPC connection
                delete(r.nodes, id)      // Remove from the map
                slog.Warn("worker eviccionado por timeout de heartbeat", "id", id)
            }
        }
        r.mu.Unlock()
        // IMPORTANT: Do not use `defer` inside an infinite loop.
        // `defer` executes when exiting the function, not when exiting the block.
        // Since this function never exits, `defer` would never execute.
        // Always use an explicit `Unlock()` inside loops.
    }
}