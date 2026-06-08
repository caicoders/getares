# Getares — Project Plan
**Distributed AI Runtime for LAN-based inference orchestration**

---

## How to read this document

This is the living guide for the project. Every issue here maps to one GitHub issue and one feature branch. When you finish an issue, close it on GitHub and open a PR to `develop`. When a sprint closes, `develop` is merged into `release` and tagged.

You will update me at the start of each issue and as you work through it. I will help you with code, Go concepts, and unblocking problems. You will not be working alone.

---

## Branching strategy

```
main           ← stable, never touched directly
release        ← end of each sprint, tagged (v0.1.0, v0.2.0...)
develop        ← integration branch, all PRs go here
feature/N-slug ← one branch per issue
```

**Rules:**
- Never commit directly to `main`, `release`, or `develop`.
- Every issue gets its own branch cut from `develop`.
- Branch name format: `feature/N-short-title` (e.g. `feature/3-worker-grpc-server`).
- PRs always target `develop`, never `main` or `release`.
- PR must compile (`go build ./...`) before merging. Tests if they exist.
- Commit messages in English, imperative mood: `Add worker Health RPC`, not `added health`.

---

## Context: what you already have

Before Sprint 1 starts, the folder structure exists (from your init script) and you have reference code from our design sessions. Sprint 1 is about turning that into something real that compiles and runs end-to-end. Nothing more.

**Your background:** Java developer learning Go via tutorials. This matters for how issues are written — Go concepts that would be obvious to a Go senior are explained where they appear for the first time.

---

## Product roadmap (high level)

| Sprint | Duration | Outcome |
|--------|----------|---------|
| Sprint 1 | 2 weeks | One request flows: `curl → coordinator → worker → llama.cpp → streaming response` |
| Sprint 2 | 2 weeks | mDNS discovery + session affinity + multi-worker routing |
| Sprint 3 | 2 weeks | Capability model + scoring scheduler + model load/unload |
| Sprint 4 | 2 weeks | Hardening, error recovery, configuration, v0.1.0 release |

---

---

# Sprint 1 — The vertical slice

**Duration:** 2 weeks

**Sprint goal:**
A real HTTP request enters the OpenAI-compatible endpoint, the coordinator forwards it to a single registered worker, the worker calls a local `llama-server` subprocess, and streaming tokens come back to the caller. No dashboard. No mDNS. No multi-worker scheduling. Just one working pipe.

**Why this goal:** Everything in Sprint 2 and beyond is worthless if this doesn't work. This sprint validates the entire architecture in one shot.

**Definition of Sprint Done:**
```
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"<your-model-id>","messages":[{"role":"user","content":"Hello"}],"stream":true}' \
  --no-buffer
```
...returns streaming tokens to the terminal.

---

## Issue #1 — Repository bootstrap and Go toolchain

**Branch:** `feature/1-repo-bootstrap`

### Description

Before writing any business logic, the repository needs a clean foundation: correct Go module name, `buf` installed and configured for proto generation, and a `Makefile` with the commands you'll use every day. This issue has no Go logic — it is pure setup. It is the most important issue because every other issue depends on it being correct.

**Go concepts introduced:**
- `go.mod` — the file that declares your module name and Go version. Think of it as `pom.xml` in Maven. The module name must match your GitHub repo path exactly.
- `buf` — a tool that reads `.proto` files and generates Go code from them. You run it once per proto change.

### Tasks

1. Confirm `go.mod` has `module github.com/idevcm/Getares` and `go 1.22` (or whatever version you have installed — check with `go version`).
2. Install `buf` following https://buf.build/docs/installation. Verify with `buf --version`.
3. Create `buf.yaml` at the repo root with this content:
   ```yaml
   version: v2
   modules:
     - path: proto
   lint:
     use:
       - DEFAULT
   ```
4. Create `buf.gen.yaml` at the repo root:
   ```yaml
   version: v2
   plugins:
     - remote: buf.build/protocolbuffers/go
       out: gen
       opt:
         - paths=source_relative
     - remote: buf.build/grpc/go
       out: gen
       opt:
         - paths=source_relative
         - require_unimplemented_servers=false
   ```
5. Add `gen/` to `.gitignore` — generated code is not committed.
6. Create `Makefile` at repo root:
   ```makefile
   .PHONY: proto build coordinator worker clean run-coordinator run-worker smoke

   proto:
   	buf generate

   build: coordinator worker

   coordinator:
   	go build -o bin/coordinator ./cmd/coordinator

   worker:
   	go build -o bin/worker ./cmd/worker

   run-coordinator:
   	go run ./cmd/coordinator --grpc :9090 --http :8080

   run-worker:
   	go run ./cmd/worker \
   		--id worker-1 \
   		--listen :9091 \
   		--llama-port 8081 \
   		--model $(MODEL) \
   		--model-id $(MODEL_ID) \
   		--coordinator localhost:9090

   smoke:
   	curl -s http://localhost:8080/v1/chat/completions \
   		-H "Content-Type: application/json" \
   		-d '{"model":"$(MODEL_ID)","messages":[{"role":"user","content":"Say hello in one sentence."}],"stream":true}' \
   		--no-buffer

   clean:
   	rm -rf bin/
   ```
7. Create `.gitignore`:
   ```
   bin/
   gen/
   *.gguf
   .env
   ```
8. Run `go mod tidy` — it will do nothing meaningful yet but confirms the toolchain works.

### Definition of done

- [ ] `go version` prints Go 1.22 or higher.
- [ ] `buf --version` prints a version number without error.
- [ ] `go.mod` first line is `module github.com/idevcm/Getares`.
- [ ] `make build` fails with a compile error (expected — no code yet), not a toolchain error.
- [ ] PR merged to `develop`.

---

## Issue #2 — Proto definitions: worker and coordinator contracts

**Branch:** `feature/2-proto-definitions`

### Description

The `.proto` files are the contract between coordinator and worker. Everything else — the Go code, the scheduling logic, the HTTP API — depends on these files being correct. Changing them later is painful because it requires regenerating code and updating every file that uses the generated types. Get them right now.

For Sprint 1, we define a simplified version of the worker proto (no `LoadModel`/`UnloadModel` yet — that is Sprint 3). We define the full coordinator proto because it is small.

**Go concepts introduced:**
- Protocol Buffers — a language-neutral way to define data structures and service contracts, similar to Java interfaces but cross-language and binary-serialized.
- `gRPC` — a framework that uses `.proto` files to generate strongly typed client and server code. Think of it as REST but binary, streaming, and type-safe.
- `service` in proto — defines the RPC methods (like an interface in Java).
- `message` in proto — defines the data structures (like a DTO in Java).

### Tasks

1. Write `proto/worker/v1/worker.proto`:
   ```protobuf
   syntax = "proto3";
   package worker.v1;
   option go_package = "github.com/idevcm/Getares/gen/worker/v1;workerv1";

   service WorkerService {
     rpc Infer(InferRequest) returns (stream InferChunk);
     rpc Health(HealthRequest) returns (HealthResponse);
   }

   message InferRequest {
     string           session_id  = 1;
     string           model_id    = 2;
     repeated Message messages    = 3;
     float            temperature = 4;
     int32            max_tokens  = 5;
   }

   message Message {
     string role    = 1;
     string content = 2;
   }

   message InferChunk {
     string token         = 1;
     bool   done          = 2;
     string finish_reason = 3;
     string error         = 4;
   }

   message HealthRequest {}

   message HealthResponse {
     string          node_id       = 1;
     string          status        = 2;
     repeated string loaded_models = 3;
   }
   ```

2. Write `proto/coordinator/v1/coordinator.proto`:
   ```protobuf
   syntax = "proto3";
   package coordinator.v1;
   option go_package = "github.com/idevcm/Getares/gen/coordinator/v1;coordinatorv1";

   service CoordinatorService {
     rpc Register(RegisterRequest)   returns (RegisterResponse);
     rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
   }

   message RegisterRequest {
     string          node_id   = 1;
     string          address   = 2;
     int32           port      = 3;
     repeated string model_ids = 4;
   }

   message RegisterResponse {
     bool   accepted = 1;
     string message  = 2;
   }

   message HeartbeatRequest {
     string node_id = 1;
   }

   message HeartbeatResponse {
     bool ok = 1;
   }
   ```

3. Add the gRPC and protobuf dependencies:
   ```bash
   go get google.golang.org/grpc@latest
   go get google.golang.org/protobuf@latest
   go mod tidy
   ```

4. Run `make proto` — this generates Go code in `gen/`. If `buf generate` fails, paste the error and we will fix it together.

5. Verify the `gen/` folder was created and contains `.go` files.

### Definition of done

- [ ] `proto/worker/v1/worker.proto` exists and matches the schema above.
- [ ] `proto/coordinator/v1/coordinator.proto` exists and matches the schema above.
- [ ] `make proto` runs without errors.
- [ ] `gen/worker/v1/` and `gen/coordinator/v1/` contain generated `.go` files.
- [ ] `go build ./gen/...` compiles without errors.
- [ ] PR merged to `develop`.

---

## Issue #3 — Worker gRPC server (Health + Infer skeleton)

**Branch:** `feature/3-worker-grpc-server`

### Description

The worker is the node that does the actual inference. It runs a gRPC server that the coordinator calls. For Sprint 1, it only needs two methods: `Health` (so the coordinator knows it exists and what it offers) and `Infer` (so it can actually respond to requests). The real inference logic (calling `llama-server`) comes in Issue #4. Here we write the server structure and the `Health` method with real data, and a placeholder `Infer` that returns a single hardcoded token — so we can verify the gRPC server works independently before connecting it to llama.

**Go concepts introduced:**
- `struct` — equivalent to a class in Java, but no inheritance. Methods are attached with a receiver `func (s *Server) Health(...)`.
- `interface` — gRPC generates an interface (e.g. `WorkerServiceServer`). Your struct must implement all methods in it. This is like `implements` in Java, but Go checks it implicitly at compile time.
- `context.Context` — passed into every gRPC method. It carries deadlines and cancellation signals. Always accept it, always forward it.
- Error handling — Go returns errors as values, not exceptions. Pattern: `if err != nil { return nil, err }`.

### Tasks

1. Write `internal/worker/server.go`. The struct `Server` embeds `workerv1.UnimplementedWorkerServiceServer` (this is the Go equivalent of implementing a Java abstract class — it provides default no-op implementations so you only need to write the methods you care about).

   ```go
   package worker

   import (
       "context"
       workerv1 "github.com/idevcm/Getares/gen/worker/v1"
   )

   type Server struct {
       workerv1.UnimplementedWorkerServiceServer
       nodeID  string
       modelID string
       llama   *LlamaServer // defined in Issue #4; use nil for now
   }

   func NewServer(nodeID, modelID string) *Server {
       return &Server{nodeID: nodeID, modelID: modelID}
   }

   func (s *Server) Health(_ context.Context, _ *workerv1.HealthRequest) (*workerv1.HealthResponse, error) {
       return &workerv1.HealthResponse{
           NodeId:       s.nodeID,
           Status:       "ready",
           LoadedModels: []string{s.modelID},
       }, nil
   }

   func (s *Server) Infer(req *workerv1.InferRequest, stream workerv1.WorkerService_InferServer) error {
       // Placeholder — replaced in Issue #4
       return stream.Send(&workerv1.InferChunk{
           Token:        "pong",
           Done:         true,
           FinishReason: "stop",
       })
   }
   ```

2. Write the worker entry point `cmd/worker/main.go`. This is where Cobra lives. It reads flags, starts the gRPC server, and registers with the coordinator. For now, skip the registration loop — just start the gRPC server and keep it running.

   ```go
   package main

   import (
       "fmt"
       "log/slog"
       "net"
       "os"

       "github.com/spf13/cobra"
       "google.golang.org/grpc"
       workerv1 "github.com/idevcm/Getares/gen/worker/v1"
       "github.com/idevcm/Getares/internal/worker"
   )

   func main() {
       var nodeID, listenAddr, modelID string

       root := &cobra.Command{
           Use:   "worker",
           Short: "Getares worker node",
           RunE: func(cmd *cobra.Command, _ []string) error {
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

       root.Flags().StringVar(&nodeID,     "id",       "worker-1", "Node identifier")
       root.Flags().StringVar(&listenAddr, "listen",   ":9091",    "gRPC listen address")
       root.Flags().StringVar(&modelID,    "model-id", "default",  "Model advertised to coordinator")

       if err := root.Execute(); err != nil {
           os.Exit(1)
       }
   }
   ```

3. Install Cobra:
   ```bash
   go get github.com/spf13/cobra@latest
   go mod tidy
   ```

4. Verify it compiles:
   ```bash
   go build ./cmd/worker
   ```

5. Run it and verify it starts:
   ```bash
   ./worker --id test-node --listen :9091 --model-id llama3
   # Should print: worker listening addr=:9091
   ```

6. Optionally verify with `grpcurl` (install from https://github.com/fullstorydev/grpcurl):
   ```bash
   grpcurl -plaintext -d '{}' localhost:9091 worker.v1.WorkerService/Health
   ```
   Should return `{"nodeId":"test-node","status":"ready","loadedModels":["llama3"]}`.

### Definition of done

- [ ] `go build ./cmd/worker` compiles without errors.
- [ ] Worker starts and prints its listen address.
- [ ] `Health` RPC returns the correct node ID, status, and model list.
- [ ] `Infer` RPC returns the `"pong"` placeholder chunk (will be replaced in #4).
- [ ] PR merged to `develop`.

---

## Issue #4 — llama-server subprocess manager

**Branch:** `feature/4-llama-server-subprocess`

### Description

This is the most technically interesting piece of Sprint 1. The worker does not run inference itself — it delegates to `llama-server`, a separate HTTP server that is part of the llama.cpp project and handles the actual model execution. The worker launches `llama-server` as a child process, waits for it to be ready (by polling its `/health` endpoint), and then proxies inference requests to it by calling its `/v1/chat/completions` endpoint. When `llama-server` returns a streaming SSE response, the worker parses each token and forwards it back to the coordinator via the gRPC stream.

This design means the worker is not tightly coupled to llama.cpp — if you later want to support ollama or another backend, you only change this file.

**Go concepts introduced:**
- `os/exec` — run external processes from Go. Like `Runtime.exec()` in Java.
- `bufio.Scanner` — read a stream line by line. Needed for SSE parsing.
- Goroutines and `go func()` — lightweight concurrency. Used here to run the gRPC server while the subprocess runs alongside it. Think of goroutines as extremely cheap threads.
- `defer` — runs a function when the surrounding function returns. Like `finally` in Java. Used here to stop llama-server when the worker shuts down.

### Tasks

1. Confirm `llama-server` is installed and on your PATH:
   ```bash
   llama-server --version
   ```
   If not installed: on Mac `brew install llama.cpp`; on Linux compile from source at https://github.com/ggerganov/llama.cpp.

2. Download a GGUF model file if you do not have one. Recommended for testing (small and fast):
   - `Phi-3-mini-4k-instruct-q4.gguf` from Hugging Face (search "phi-3 mini gguf").
   - Save it anywhere on your machine. Note the full path.

3. Write `internal/worker/inference.go` with the full content from the Sprint 1 code we generated earlier. This file contains: `LlamaServer` struct, `StartLlamaServer()`, `waitReady()`, `Stop()`, `Infer()`, and the SSE parser `parseSSE()`.

4. Update `internal/worker/server.go` to accept a `*LlamaServer` and use it in `Infer`:
   - Add `llama *LlamaServer` field to `Server`.
   - Update `NewServer` signature: `func NewServer(nodeID, modelID string, llama *LlamaServer) *Server`.
   - Replace the placeholder `Infer` with: `return s.llama.Infer(stream.Context(), req, stream)`.

5. Update `cmd/worker/main.go` to:
   - Add `--model` flag (path to GGUF file) and `--llama-port` flag (default `8081`).
   - Call `worker.StartLlamaServer(modelPath, llamaPort)` before starting the gRPC server.
   - Pass the `*LlamaServer` to `worker.NewServer(...)`.
   - Call `llama.Stop()` with `defer` so it cleans up on exit.

6. Compile and test manually:
   ```bash
   go build ./cmd/worker
   ./worker \
     --id worker-1 \
     --listen :9091 \
     --llama-port 8081 \
     --model /path/to/your-model.gguf \
     --model-id phi3
   ```
   You should see llama-server start and eventually: `llama-server ready`.

7. Test inference directly against llama-server (bypass the worker for now) to confirm your model works:
   ```bash
   curl -s http://localhost:8081/v1/chat/completions \
     -H "Content-Type: application/json" \
     -d '{"model":"phi3","messages":[{"role":"user","content":"Say hello"}],"stream":true}' \
     --no-buffer
   ```

### Definition of done

- [ ] `go build ./cmd/worker` compiles with the new flags.
- [ ] Worker starts llama-server subprocess and prints `llama-server ready`.
- [ ] Direct curl to llama-server (port 8081) returns streaming tokens.
- [ ] `Infer` gRPC call (via grpcurl or a test) returns real tokens, not `"pong"`.
- [ ] Worker shuts down cleanly (llama-server subprocess is killed on exit).
- [ ] PR merged to `develop`.

---

## Issue #5 — Coordinator: registry and gRPC server

**Branch:** `feature/5-coordinator-registry`

### Description

The coordinator is the brain of the cluster. It maintains a registry of active workers (which node exists, what models it has, when it last checked in). For Sprint 1, it does the minimum: accept worker registrations, accept heartbeats, evict workers that stop sending heartbeats, and when asked to route a request, pick any available worker that has the right model. No scoring. No session affinity. Just "find a worker and use it."

The TTL eviction is important even in Sprint 1 — without it, a worker that crashes would remain in the registry forever and cause all future requests to fail.

**Go concepts introduced:**
- `sync.RWMutex` — a lock that allows multiple concurrent readers but only one writer. Like `ReadWriteLock` in Java. Use `RLock()`/`RUnlock()` for reads, `Lock()`/`Unlock()` for writes.
- `map` — Go's built-in hash map. `map[string]*Node` maps a string key to a pointer to Node.
- `time.Tick` — returns a channel that receives the current time at a fixed interval. Used for the eviction loop.
- `go func()` — starts the eviction loop as a background goroutine.

### Tasks

1. Write `internal/coordinator/registry.go` with the full content from the Sprint 1 code we generated. This file contains: `Node` struct, `Registry` struct, `NewRegistry()`, `Add()`, `Touch()`, `Pick()`, and the background `evict()` goroutine. Review it carefully — `Pick()` opens a real gRPC connection to the worker using the address stored at registration.

2. Write `internal/coordinator/server.go`:
   ```go
   package coordinator

   import (
       "context"
       coordinatorv1 "github.com/idevcm/Getares/gen/coordinator/v1"
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
   ```

3. Write `cmd/coordinator/main.go` using the content from the Sprint 1 code we generated. It starts two servers concurrently: the gRPC server (for workers) and the HTTP server (for clients). The HTTP server is wired in Issue #6 — for now, use a placeholder `http.HandlerFunc` that returns `501 Not Implemented`.

4. Compile:
   ```bash
   go build ./cmd/coordinator
   ```

5. Start coordinator and worker in two terminals. In a third terminal, verify the worker registered:
   ```bash
   # No endpoint for this yet — check coordinator logs for "worker registered"
   ```

### Definition of done

- [ ] `go build ./cmd/coordinator` compiles without errors.
- [ ] Coordinator starts and logs `coordinator gRPC listening` and `coordinator HTTP API listening`.
- [ ] When a worker starts with `--coordinator localhost:9090`, coordinator logs `worker registered`.
- [ ] When the worker is stopped and does not send heartbeats, coordinator logs `worker evicted` after 15 seconds.
- [ ] PR merged to `develop`.

---

## Issue #6 — Worker registration and heartbeat loop

**Branch:** `feature/6-worker-registration-loop`

### Description

Right now the worker starts a gRPC server but does not connect to the coordinator. This issue adds the registration and heartbeat loop to the worker. The loop must be resilient: if the coordinator is not reachable at startup (because it started a second later), the worker retries. If the coordinator crashes and recovers, the worker automatically re-registers on the next heartbeat cycle.

This is a good example of the coordinator failure tolerance we designed: workers are self-healing.

**Go concepts introduced:**
- `for range time.Tick(...)` — loops forever, executing on each tick interval. This is the heartbeat loop pattern in Go.
- `net.Dial("udp", ...)` — used to discover the machine's outbound IP address. A common Go trick: no packet is actually sent, but the OS routing table is consulted to determine which interface would be used.
- Blocking vs non-blocking: `srv.Serve(lis)` blocks. That is why it must run in a goroutine, and the heartbeat loop runs in the main goroutine.

### Tasks

1. Add `registerLoop()` function to `cmd/worker/main.go` (or extract to `internal/worker/registration.go` if you prefer). The full implementation is in the Sprint 1 code we generated. Key behaviours:
   - Dial the coordinator gRPC address.
   - Call `Register` in a loop until it succeeds (`resp.Accepted == true`). Sleep 5s between retries.
   - Detect the worker's LAN IP with `outboundIP()` so the coordinator can call back.
   - After successful registration, enter the heartbeat loop: call `Heartbeat` every 5 seconds.

2. Update `cmd/worker/main.go` to:
   - Add `--coordinator` flag (default: `localhost:9090`).
   - Start the gRPC server in a goroutine (`go srv.Serve(lis)`).
   - Call `registerLoop(...)` in the main goroutine (blocking — it runs forever).

3. Test the resilience manually:
   - Start the worker before the coordinator. Watch it retry.
   - Start the coordinator. Watch the worker register.
   - Kill the coordinator (`Ctrl+C`). Wait 15s. Watch the coordinator log `worker evicted` when it comes back.
   - Restart the coordinator. Watch the worker re-register automatically.

### Definition of done

- [ ] Worker retries registration if coordinator is unreachable at startup.
- [ ] Worker sends heartbeats every 5 seconds; coordinator logs confirm receipt.
- [ ] Coordinator evicts the worker after 15s of missed heartbeats.
- [ ] Worker re-registers automatically after coordinator restart.
- [ ] PR merged to `develop`.

---

## Issue #7 — OpenAI-compatible HTTP API

**Branch:** `feature/7-openai-http-api`

### Description

This is the last piece of Sprint 1. Clients (curl, VS Code, Continue.dev) talk HTTP, not gRPC. This issue adds an HTTP handler that translates incoming OpenAI-format requests into gRPC calls to the coordinator's registry, then streams the gRPC response back as Server-Sent Events (SSE).

The coordinator is not a proxy in the classic sense — it holds the gRPC clients to workers in the registry and dispatches directly. The HTTP layer sits on top of the coordinator and uses the registry to find a worker client.

**Go concepts introduced:**
- `http.ServeMux` — Go's built-in router. Routes by path pattern. Sufficient for Sprint 1 — no external framework needed.
- `http.Flusher` — an interface that, when implemented by a `ResponseWriter`, allows flushing the buffer immediately. Required for SSE streaming.
- `encoding/json` — standard library for JSON encode/decode. Like Jackson in Java.
- SSE format — Server-Sent Events. Each event is `data: <json>\n\n`. The special event `data: [DONE]\n\n` signals end of stream.

### Tasks

1. Write `internal/api/openai/server.go` with the full content from the Sprint 1 code we generated. This file contains the HTTP handler, the SSE writer, the JSON collector for non-streaming mode, and the OpenAI request/response types.

2. Wire the HTTP handler into `cmd/coordinator/main.go`:
   - Replace the placeholder `501` handler with `openai.NewHandler(reg)`.
   - Pass the registry `reg` to `openai.NewHandler`.

3. Compile the full project:
   ```bash
   go build ./...
   ```
   This builds everything. Fix any import errors — they are usually missing or wrong module paths.

4. Run the full stack:
   - Terminal 1: `make run-coordinator`
   - Terminal 2: `make run-worker MODEL=/path/to/model.gguf MODEL_ID=phi3`
   - Terminal 3: `make smoke MODEL_ID=phi3`

5. If the smoke test returns tokens, Sprint 1 is done. If it does not, we debug together — paste the error.

6. Test with VS Code + Continue.dev extension:
   - In Continue settings, add a model with provider "openai", base URL `http://localhost:8080`, model name = your model ID.
   - Open a file and try autocomplete or chat.

### Definition of done

- [ ] `go build ./...` compiles the entire project without errors.
- [ ] `make smoke` returns streaming tokens in the terminal.
- [ ] Non-streaming mode (`"stream": false`) returns a valid JSON object with `choices[0].message.content`.
- [ ] `GET /v1/models` returns a 200 response (empty list is fine).
- [ ] VS Code or another OpenAI-compatible client connects and returns a response.
- [ ] PR merged to `develop`.

---

## Issue #8 — Sprint 1 release: tag v0.1.0-alpha

**Branch:** `feature/8-sprint1-release`

### Description

Sprint 1 is complete. This issue closes it formally: update the README with real setup instructions (based on what you actually ran, not what we planned), merge `develop` into `release`, and tag it.

### Tasks

1. Update `README.md` with:
   - What Getares is (one paragraph).
   - Prerequisites (Go version, `buf`, `llama-server`, a GGUF model).
   - How to build (`make build`).
   - How to run coordinator and worker.
   - The smoke test `curl` command.
   - Known limitations (no mDNS, no scheduling, no HA).

2. Run the full smoke test one final time from a clean build:
   ```bash
   make clean && make build
   ```

3. Merge `develop` → `release`:
   ```bash
   git checkout release
   git merge develop
   git tag v0.1.0-alpha
   git push origin release --tags
   ```

4. Open a GitHub Release from the tag with a short description of what works.

### Definition of done

- [ ] README is accurate and runnable by someone who was not involved in the project.
- [ ] `make clean && make build` produces working binaries.
- [ ] `release` branch is tagged `v0.1.0-alpha`.
- [ ] GitHub Release created.
- [ ] PR merged.

---

---

# Sprint 2 — Make it a real cluster

**Duration:** 2 weeks (planned, to be refined at Sprint 1 retrospective)

**Sprint goal:**
Multiple workers join the cluster automatically via mDNS. Requests from the same session always go to the same worker (KV-cache locality). The coordinator picks the best worker based on a simple scoring function instead of "first available."

**Why this goal:** Sprint 1 proved the plumbing works. Sprint 2 makes Getares actually useful as a distributed system.

**Issues (to be detailed at Sprint 1 close):**

| # | Title | Description |
|---|-------|-------------|
| #9 | mDNS announce (worker) | Worker broadcasts its presence on the LAN using `hashicorp/mdns`. No manual `--coordinator` flag needed. |
| #10 | mDNS browse (coordinator) | Coordinator listens for mDNS announcements and auto-registers workers it discovers. |
| #11 | Session affinity | Coordinator maintains a `sessionID → nodeID` map with TTL eviction. Requests with the same `X-Session-Id` header are routed to the same worker while it is healthy. |
| #12 | Capability-aware scheduler | Replace `Pick()` (first available) with the scoring function from `architecture-decisions.md`. Score on model loaded, session affinity, active sessions, queued requests. |
| #13 | `/v1/models` endpoint | Coordinator aggregates loaded models from all registered workers and returns a real model list. |
| #14 | Sprint 2 release: v0.2.0-alpha | README update, tag, release notes. |

---

# Sprint 3 — Capability model and model lifecycle

**Duration:** 2 weeks (to be refined at Sprint 2 retrospective)

**Sprint goal:**
Workers report VRAM, RAM, and GPU info. The coordinator can instruct workers to load or unload models. Requests for a model not loaded anywhere trigger an automatic load on the most capable worker.

**Issues (to be detailed at Sprint 2 close):**

| # | Title | Description |
|---|-------|-------------|
| #15 | Node capability proto | Add `capability.proto` with `NodeCapability` and `NodeLoad`. Update `coordinator.proto` heartbeat to carry `NodeLoad`. |
| #16 | Worker capability reporting | Worker detects available RAM and GPU info (using `runtime` package and optional `nvidia-smi` for GPU). Reports on registration and heartbeat. |
| #17 | `LoadModel` RPC (worker) | Worker implements `LoadModel` — instructs llama-server to load a model. Returns synchronously when done or fails. Note: adds loading time, tracked with a `loading` state flag. |
| #18 | `UnloadModel` RPC (worker) | Worker implements `UnloadModel` — instructs llama-server to unload a model and free memory. |
| #19 | Load-on-demand scheduling | Coordinator detects when no worker has the requested model loaded, picks the best candidate by VRAM, and calls `LoadModel` before dispatching. |
| #20 | Sprint 3 release: v0.3.0-alpha | README update, tag, release notes. |

---

# Sprint 4 — Hardening and v0.1.0 release

**Duration:** 2 weeks (to be refined at Sprint 3 retrospective)

**Sprint goal:**
Getares is stable enough to use as a daily driver in a small team. Error messages are clear. The worker recovers from llama-server crashes. Configuration is flexible. The project has a real README.

**Issues (to be detailed at Sprint 3 close):**

| # | Title | Description |
|---|-------|-------------|
| #21 | Worker crash recovery | Detect llama-server process death. Restart it automatically with backoff. Return a clear error to clients during recovery. |
| #22 | Configuration file support | Support a YAML config file alongside CLI flags (via `spf13/viper`). Useful for teams deploying multiple workers. |
| #23 | Structured error responses | All error paths return valid OpenAI-format error JSON, not plain text HTTP errors. |
| #24 | Integration test suite | End-to-end test that starts coordinator + mock worker and verifies the request flow without needing a real GPU. |
| #25 | Project README and docs | Full README: architecture diagram, installation, configuration reference, troubleshooting. |
| #26 | v0.1.0 release | Merge to `main`. Tag `v0.1.0`. Publish GitHub Release with binaries for Linux (amd64, arm64) and macOS (arm64). |

---

## Notes for working together

**When you start an issue:** Tell me the issue number and branch name. I will brief you on what to focus on and flag any Go concepts you will encounter.

**When you are stuck:** Paste the error, the file you are editing, and what you tried. Do not spend more than 20 minutes on the same error alone.

**When you finish an issue:** Run the definition of done checklist before opening the PR. One unchecked item is fine if you know why. Zero understanding of what you built is not fine.

**When a sprint closes:** We do a short retrospective — what took longer than expected, what was easier, and whether the next sprint scope is right.

**Go learning path alongside this project:** Since this is your first Go project, I recommend running through these topics in parallel with the sprints, in this order: (1) Go tour — https://go.dev/tour — do the basics and concurrency sections; (2) Error handling patterns; (3) Goroutines and channels; (4) `net/http` stdlib. You will encounter each of these in Sprint 1 and 2.
