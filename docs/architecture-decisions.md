# Architecture decisions — distributed AI runtime

## 1. Coordinator failure handling (v1)

**Decision: stateless tolerance — no leader election.**

In v1, the coordinator is a single process. If it crashes, workers continue serving
in-flight sessions until completion, then queue new requests locally with a small
backpressure buffer. Routing stops; inference does not.

Recovery is instant: the coordinator restarts, workers re-register via their next
heartbeat cycle, and the cluster reforms within one heartbeat interval (default 5 s).
Sessions that were in-flight during the outage are lost at the coordinator level (no
session affinity after recovery), but the client receives a clear error and can retry.

**What this avoids:** Raft, etcd, ZooKeeper, split-brain detection — none of that
belongs in v1 of a LAN tool. The tradeoff is acceptable because:

- The coordinator is a Go process with ~10 MB idle RSS. Restart is sub-second.
- LAN tools have a human operator nearby.
- Session loss on coordinator crash is a minor UX issue, not a data integrity issue.

**Future path:** If HA becomes a requirement, promote the coordinator's state store to
a replicated KV (e.g. embedded Raft via `hashicorp/raft`) without changing the worker
contract. The worker protocol is coordinator-agnostic.

---

## 2. Worker gRPC contract

### `worker.proto`

```protobuf
syntax = "proto3";
package runtime.worker.v1;
option go_package = "github.com/idevcm/runtime/proto/worker/v1;workerv1";

// ── Core service ──────────────────────────────────────────────────────────────

service Worker {
  // Streaming inference. The response is a stream of token chunks.
  rpc Infer(InferRequest) returns (stream InferChunk);

  // Returns current capabilities and load. Called by the coordinator
  // on registration and periodically to refresh the scheduling state.
  rpc Health(HealthRequest) returns (HealthResponse);

  // Instructs the worker to load a model into memory (VRAM or RAM).
  // Blocking until the model is ready or an error occurs.
  rpc LoadModel(LoadModelRequest) returns (LoadModelResponse);

  // Instructs the worker to unload a model and free its memory.
  rpc UnloadModel(UnloadModelRequest) returns (UnloadModelResponse);
}

// ── Infer ─────────────────────────────────────────────────────────────────────

message InferRequest {
  string session_id  = 1; // Sticky routing key. Same session → same node.
  string model_id    = 2; // Must match a loaded model on this worker.
  repeated Message messages = 3;
  InferParams params = 4;
}

message Message {
  string role    = 1; // "system" | "user" | "assistant"
  string content = 2;
}

message InferParams {
  float temperature = 1;
  int32 max_tokens  = 2;
  float top_p       = 3;
  int32 seed        = 4; // 0 = random
}

message InferChunk {
  string token         = 1;
  bool   done          = 2;
  string finish_reason = 3; // "stop" | "length" | "error"
  string error         = 4; // non-empty only when finish_reason = "error"
}

// ── Health ────────────────────────────────────────────────────────────────────

message HealthRequest {}

message HealthResponse {
  string         node_id    = 1;
  NodeCapability capability = 2;
  NodeLoad       load       = 3;
}

// ── LoadModel / UnloadModel ───────────────────────────────────────────────────

message LoadModelRequest {
  string model_id   = 1; // Logical name, e.g. "llama3-8b-q4"
  string model_path = 2; // Absolute path on the worker's filesystem
}

message LoadModelResponse {
  bool   success = 1;
  string error   = 2;
}

message UnloadModelRequest {
  string model_id = 1;
}

message UnloadModelResponse {
  bool   success = 1;
  string error   = 2;
}
```

---

## 3. Node capability model

### `capability.proto`

```protobuf
syntax = "proto3";
package runtime.capability.v1;
option go_package = "github.com/idevcm/runtime/proto/capability/v1;capabilityv1";

// NodeCapability is static or slow-changing hardware description.
// Sent on registration and refreshed only when hardware changes.
message NodeCapability {
  string node_id  = 1;
  string hostname = 2;
  string os       = 3;   // "linux" | "windows" | "darwin"
  string arch     = 4;   // "amd64" | "arm64"

  uint64 total_ram_bytes = 5;
  repeated GPUInfo gpus  = 6;

  // Models present on disk (not necessarily loaded).
  repeated string available_model_ids = 7;

  // Models currently loaded and ready to serve.
  repeated LoadedModel loaded_models = 8;
}

message GPUInfo {
  int32  index             = 1;
  string name              = 2; // e.g. "NVIDIA GeForce RTX 3090"
  uint64 total_vram_bytes  = 3;
}

message LoadedModel {
  string model_id         = 1;
  uint64 vram_usage_bytes = 2; // 0 if CPU-only
  uint64 ram_usage_bytes  = 3;
  bool   ready            = 4;
}

// NodeLoad is fast-changing runtime state.
// Sent with every heartbeat.
message NodeLoad {
  float  cpu_usage_percent      = 1; // 0.0–100.0
  uint64 available_ram_bytes    = 2;
  repeated GPULoad gpu_loads    = 3;
  uint32 active_sessions        = 4;
  uint32 queued_requests        = 5;
}

message GPULoad {
  int32  index                  = 1;
  uint64 available_vram_bytes   = 2;
  float  utilization_percent    = 3; // 0.0–100.0
}
```

### `coordinator.proto`

```protobuf
syntax = "proto3";
package runtime.coordinator.v1;
option go_package = "github.com/idevcm/runtime/proto/coordinator/v1;coordinatorv1";

import "capability/v1/capability.proto";

// Workers call this service to join and maintain cluster membership.
service Coordinator {
  // Called once on startup (and after coordinator recovery).
  rpc Register(RegisterRequest) returns (RegisterResponse);

  // Called every heartbeat interval (default 5 s).
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

message RegisterRequest {
  string                          node_id    = 1;
  string                          address    = 2; // IP or hostname
  int32                           port       = 3; // gRPC port
  runtime.capability.v1.NodeCapability capability = 4;
}

message RegisterResponse {
  bool   accepted         = 1;
  string cluster_id       = 2;
  int32  heartbeat_interval_sec = 3; // Coordinator tells workers their interval.
}

message HeartbeatRequest {
  string                       node_id = 1;
  runtime.capability.v1.NodeLoad load  = 2;
  // Diff: models loaded/unloaded since last heartbeat.
  repeated string loaded_model_ids     = 3;
  repeated string unloaded_model_ids   = 4;
}

message HeartbeatResponse {
  bool ok = 1;
  // Coordinator can push lightweight commands back on heartbeat.
  repeated CoordinatorCommand commands = 2;
}

// Thin command channel — avoids a separate push stream for simple ops.
message CoordinatorCommand {
  oneof command {
    LoadModelCommand   load_model   = 1;
    UnloadModelCommand unload_model = 2;
  }
}

message LoadModelCommand   { string model_id = 1; string model_path = 2; }
message UnloadModelCommand { string model_id = 1; }
```

---

## 4. Scheduling logic (coordinator internal)

The coordinator scores candidate nodes for each request using a simple priority function:

```
score(node, request) =
  model_loaded(node, request.model_id)     × 1000   // hard requirement
  + session_affinity(node, request.session) × 500    // prefer existing session
  - active_sessions(node)                  × 10     // penalize busy nodes
  - queued_requests(node)                  × 50     // penalize queued nodes
  + available_vram(node)                   × 0.001  // prefer headroom (bytes)
```

If no node has the model loaded, the coordinator picks the node with the most
available VRAM and issues a `LoadModel` RPC before forwarding the request.
This load-on-demand path adds latency only on the first request for a model on
a given node.

---

## 5. Repository layout

```
/
├── cmd/
│   ├── coordinator/
│   │   └── main.go          # cobra root: coordinator start
│   └── worker/
│       └── main.go          # cobra root: worker start --coordinator <addr>
│
├── internal/
│   ├── coordinator/
│   │   ├── server.go        # gRPC server (Register, Heartbeat)
│   │   ├── registry.go      # in-memory node registry + TTL eviction
│   │   ├── scheduler.go     # scoring + dispatch
│   │   └── session.go       # sessionID → nodeID map
│   ├── worker/
│   │   ├── server.go        # gRPC server (Infer, Health, Load, Unload)
│   │   ├── inference.go     # llama.cpp subprocess management
│   │   └── models.go        # local model inventory
│   ├── discovery/
│   │   └── mdns.go          # mDNS announce + browse (hashicorp/mdns)
│   └── api/
│       └── openai/
│           ├── server.go    # HTTP server (chi or net/http)
│           ├── chat.go      # POST /v1/chat/completions → coordinator
│           └── models.go    # GET /v1/models
│
├── proto/
│   ├── worker/v1/
│   │   └── worker.proto
│   ├── capability/v1/
│   │   └── capability.proto
│   └── coordinator/v1/
│       └── coordinator.proto
│
├── pkg/
│   └── capability/
│       └── score.go         # exported scoring helpers (testable)
│
├── Makefile                 # proto gen, build, test
├── go.mod
└── README.md
```

---

## 6. Key dependencies

| Concern | Package | Reason |
|---|---|---|
| CLI | `spf13/cobra` | Specified in requirements |
| gRPC | `google.golang.org/grpc` | Core transport |
| Protobuf | `google.golang.org/protobuf` | Serialization |
| mDNS | `hashicorp/mdns` | Zero-config LAN discovery |
| HTTP API | `net/http` (stdlib) | OpenAI-compat layer, no framework |
| Logging | `log/slog` (stdlib, Go 1.21+) | Structured, zero deps |
| Config | `spf13/viper` | Optional — only if needed |

**Explicitly avoided:** Kubernetes client, service mesh, message broker,
distributed KV store, ORM, heavy web framework.

---

## 7. What to build first

Recommended build order to get to a working end-to-end demo fast:

1. **Proto schemas** — compile, generate Go bindings, commit generated code.
2. **Worker server** — `Health` + `Infer` only. Hard-code a single model. Verify
   with `grpcurl`.
3. **Coordinator** — `Register` + `Heartbeat` + in-memory registry. No scheduling
   yet — forward all requests to the single registered worker.
4. **OpenAI API layer** — `POST /v1/chat/completions` proxied through coordinator.
   Test with `curl` and VS Code / Continue.dev.
5. **mDNS** — worker announces itself; coordinator browses and auto-registers.
6. **Scheduling** — add the scoring function. Test with two workers, two models.
7. **Session affinity** — add the session → node map. Verify KV-cache locality.
8. **`LoadModel` / `UnloadModel`** — close the loop on load-on-demand scheduling.
