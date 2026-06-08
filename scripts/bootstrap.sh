#!/bin/bash

# =============================================================================
# Getares — Bootstrap Script (Omarchy / Arch Linux edition)
# Run once from the root of your cloned repo after: go mod init github.com/idevcm/Getares
# =============================================================================

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

step()  { echo -e "\n${BLUE}▶  $1${NC}"; }
ok()    { echo -e "${GREEN}   ✓ $1${NC}"; }
warn()  { echo -e "${YELLOW}   ⚠ $1${NC}"; }
fatal() { echo -e "${RED}   ✗ $1${NC}"; exit 1; }

export PATH="$PATH:$(go env GOPATH)/bin"

# =============================================================================
# 0. go.mod must already exist
# =============================================================================

step "Detecting module name"

[[ -f go.mod ]] || fatal "go.mod not found. Run: go mod init github.com/idevcm/Getares"

MODULE=$(head -1 go.mod | awk '{print $2}')
[[ -n "$MODULE" ]] || fatal "Could not read module name from go.mod"

ok "Module: $MODULE"

# =============================================================================
# 1. Go version check
# =============================================================================

step "Checking Go version"

GO_MINOR=$(go version | grep -oE 'go1\.[0-9]+' | grep -oE '[0-9]+$')
[[ "$GO_MINOR" -ge 22 ]] || fatal "Go 1.22+ required. You have: $(go version)"

ok "$(go version)"

# =============================================================================
# 2. DNS check — Omarchy's systemd-resolved often breaks Go proxy
# =============================================================================

step "Checking DNS resolution"

if ! nslookup proxy.golang.org 8.8.8.8 &>/dev/null; then
  warn "DNS resolution failing. Attempting to fix /etc/resolv.conf..."

  if [[ -L /etc/resolv.conf ]]; then
    sudo rm /etc/resolv.conf
    printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" | sudo tee /etc/resolv.conf > /dev/null
    ok "resolv.conf replaced (was a symlink to systemd-resolved)"
  else
    printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" | sudo tee /etc/resolv.conf > /dev/null
    ok "resolv.conf updated"
  fi

  sudo systemctl disable --now systemd-resolved 2>/dev/null || true
fi

nslookup proxy.golang.org 8.8.8.8 &>/dev/null \
  && ok "DNS resolves proxy.golang.org" \
  || fatal "DNS still failing. Check your network connection and re-run."

# =============================================================================
# 3. Install Go tools via "go install" — no pacman needed
# =============================================================================

step "Installing Go-based tools (go install — no package manager needed)"

# buf
if ! command -v buf &>/dev/null; then
  echo "   Installing buf binary from GitHub releases..."
  GOBIN="$(go env GOPATH)/bin"
  BUF_VERSION="1.70.0"
  ARCH=$(uname -m)
  case "$ARCH" in
    aarch64|arm64) BUF_ARCH="aarch64" ;;
    *)             BUF_ARCH="x86_64"  ;;
  esac
  BUF_URL="https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-${BUF_ARCH}"
  curl -fsSL "$BUF_URL" -o "$GOBIN/buf"
  chmod +x "$GOBIN/buf"
  ok "buf installed: $(buf --version)"
else
  ok "buf already installed: $(buf --version)"
fi

# grpcurl — installed via go install, never needs pacman
if ! command -v grpcurl &>/dev/null; then
  echo "   Installing grpcurl via go install..."
  GOPROXY="https://proxy.golang.org,direct" \
    go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
  ok "grpcurl installed"
else
  ok "grpcurl already installed"
fi

# llama-server — NOT a Go binary, needs pacman (install later when mirrors work)
if ! command -v llama-server &>/dev/null; then
  warn "llama-server not found. You need it for Issue #4 (not before)."
  warn "Install when pacman mirrors recover:"
  warn "   sudo pacman -Sy llama-cpp"
  warn "Or build from source: https://github.com/ggerganov/llama.cpp"
else
  ok "llama-server already installed: $(llama-server --version 2>&1 | head -1)"
fi

# =============================================================================
# 4. Directory structure
# =============================================================================

step "Creating directory structure"

mkdir -p \
  cmd/coordinator \
  cmd/worker \
  internal/coordinator \
  internal/worker \
  internal/discovery \
  internal/api/openai \
  proto/worker/v1 \
  proto/coordinator/v1 \
  pkg/capability \
  bin

ok "Directories created"

# =============================================================================
# 5. .gitignore
# =============================================================================

step "Writing .gitignore"

cat > .gitignore << 'GITIGNORE'
bin/
gen/
*.gguf
*.bin
*.safetensors
.env
.env.local
.DS_Store
.idea/
.vscode/settings.json
*.swp
GITIGNORE

ok ".gitignore written"

# =============================================================================
# 6. Makefile
# =============================================================================

step "Writing Makefile"

cat > Makefile << 'MAKEFILE'
.PHONY: proto build coordinator worker clean run-coordinator run-worker smoke check

proto:
	buf generate

build: coordinator worker

coordinator:
	go build -o bin/coordinator ./cmd/coordinator

worker:
	go build -o bin/worker ./cmd/worker

check:
	go build ./...
	go vet ./...

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
		-H "X-Session-Id: smoke-test-1" \
		-d '{"model":"$(MODEL_ID)","messages":[{"role":"user","content":"Say hello in one sentence."}],"stream":true}' \
		--no-buffer

clean:
	rm -rf bin/
MAKEFILE

ok "Makefile written"

# =============================================================================
# 7. buf config
# =============================================================================

step "Writing buf.yaml and buf.gen.yaml"

cat > buf.yaml << 'BUFYAML'
version: v2
modules:
  - path: proto
lint:
  use:
    - DEFAULT
breaking:
  use:
    - FILE
BUFYAML

cat > buf.gen.yaml << 'BUFGEN'
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
BUFGEN

ok "buf.yaml and buf.gen.yaml written"

# =============================================================================
# 8. Proto files
#    go_package uses printf so $MODULE expands. The rest uses single-quoted
#    heredocs so proto field numbers ($1, $2) are never interpreted by bash.
# =============================================================================

step "Writing proto definitions"

# ── worker.proto ──────────────────────────────────────────────────────────────
printf 'syntax = "proto3";\n\npackage worker.v1;\n\noption go_package = "%s/gen/worker/v1;workerv1";\n' \
  "$MODULE" > proto/worker/v1/worker.proto

cat >> proto/worker/v1/worker.proto << 'WORKER_PROTO'

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
WORKER_PROTO

# ── coordinator.proto ─────────────────────────────────────────────────────────
printf 'syntax = "proto3";\n\npackage coordinator.v1;\n\noption go_package = "%s/gen/coordinator/v1;coordinatorv1";\n' \
  "$MODULE" > proto/coordinator/v1/coordinator.proto

cat >> proto/coordinator/v1/coordinator.proto << 'COORDINATOR_PROTO'

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
COORDINATOR_PROTO

ok "Proto files written — go_package: $MODULE/gen/..."

# =============================================================================
# 9. Go stubs — correct package name + TODO → issue number
# =============================================================================

step "Writing Go stubs"

cat > cmd/coordinator/main.go << 'EOF'
package main

// TODO Issue #5 + #7 — coordinator gRPC server + OpenAI HTTP handler.

func main() {}
EOF

cat > cmd/worker/main.go << 'EOF'
package main

// TODO Issue #3 + #4 + #6 — worker gRPC server, llama-server subprocess, heartbeat loop.

func main() {}
EOF

cat > internal/coordinator/registry.go << 'EOF'
package coordinator

// TODO Issue #5 — in-memory node registry with sync.RWMutex + TTL eviction.
EOF

cat > internal/coordinator/server.go << 'EOF'
package coordinator

// TODO Issue #5 — CoordinatorService gRPC server (Register, Heartbeat).
EOF

cat > internal/coordinator/scheduler.go << 'EOF'
package coordinator

// TODO Issue #12 (Sprint 2) — capability-aware scoring scheduler.
EOF

cat > internal/coordinator/session.go << 'EOF'
package coordinator

// TODO Issue #11 (Sprint 2) — session affinity map: sessionID → nodeID + TTL.
EOF

cat > internal/worker/server.go << 'EOF'
package worker

// TODO Issue #3 — WorkerService gRPC server (Health, Infer).
EOF

cat > internal/worker/inference.go << 'EOF'
package worker

// TODO Issue #4 — LlamaServer struct: subprocess manager + SSE parser.
EOF

cat > internal/worker/models.go << 'EOF'
package worker

// TODO Issue #16 (Sprint 3) — local model inventory + capability reporting.
EOF

cat > internal/discovery/mdns.go << 'EOF'
package discovery

// TODO Issues #9 + #10 (Sprint 2) — mDNS announce (worker) + browse (coordinator).
EOF

cat > internal/api/openai/server.go << 'EOF'
package openai

// TODO Issue #7 — OpenAI HTTP handler: POST /v1/chat/completions + GET /v1/models.
EOF

cat > pkg/capability/score.go << 'EOF'
package capability

// TODO Issue #12 (Sprint 2) — exported node scoring function (unit-testable).
EOF

ok "Go stubs written"

# =============================================================================
# 10. Go dependencies
# =============================================================================

step "Installing Go dependencies"

install_dep() {
  local pkg=$1
  echo "   go get $pkg"
  GOPROXY="https://proxy.golang.org,direct" go get "$pkg" 2>/dev/null \
    || GOPROXY=direct go get "$pkg"
}

install_dep "github.com/spf13/cobra@latest"
ok "cobra"

install_dep "google.golang.org/grpc@latest"
ok "grpc"

install_dep "google.golang.org/protobuf@latest"
ok "protobuf"

go mod tidy
ok "go mod tidy — go.sum updated"

# =============================================================================
# 11. buf generate
# =============================================================================

step "Generating Go code from .proto files"

if buf generate; then
  ok "gen/ directory created"
else
  warn "buf generate failed — retry with: make proto"
fi

# =============================================================================
# 12. Final compile check
# =============================================================================

step "Verifying full project compiles"

if go build ./...; then
  ok "go build ./... — clean"
else
  warn "Compile errors above. Fix: make proto && go build ./..."
fi

# =============================================================================
# 13. Summary
# =============================================================================

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Bootstrap complete                                         ${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
printf "  %-15s %s\n" "Module:"       "$MODULE"
printf "  %-15s %s\n" "Go:"           "$(go version | awk '{print $3}')"
printf "  %-15s %s\n" "buf:"          "$(buf --version 2>/dev/null || echo 'not found')"
printf "  %-15s %s\n" "grpcurl:"      "$(grpcurl --version 2>&1 | head -1 || echo 'not found')"
printf "  %-15s %s\n" "llama-server:" "$(llama-server --version 2>&1 | head -1 || echo 'not installed — needed for Issue #4')"
echo ""
echo "  Next steps:"
echo ""
echo "  1. Commit bootstrap:"
echo "       git add ."
echo '       git commit -m "chore: bootstrap project structure"'
echo "       git push origin develop"
echo ""
echo "  2. Close Issue #1 on GitHub."
echo ""
echo "  3. Start Issue #2:"
echo "       git checkout -b feature/2-proto-definitions"
echo "       make proto    # should already be done — verify gen/ exists"
echo "       make check    # must compile clean"
echo ""
echo "  4. Install llama-server when pacman mirrors recover:"
echo "       sudo pacman -Sy llama-cpp"
echo "     (needed for Issue #4, not before)"
echo ""
echo "  Everyday commands:"
echo "    make proto               — regenerate after editing a .proto"
echo "    make check               — compile + vet"
echo "    make build               — produce bin/coordinator + bin/worker"
echo "    make smoke MODEL_ID=phi3 — end-to-end test (Sprint 1 finish line)"
echo ""