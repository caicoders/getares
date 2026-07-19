# ─────────────────────────────────────────────────────────────────────────────
# Getares Makefile
# Usage:
#   make proto                         — regenerate Go code from .proto files
#   make build                         — compile coordinator + worker binaries
#   make run-coordinator               — start coordinator (dev mode)
#   make run-worker MODEL=... MODEL_ID=...  — start worker (dev mode)
#   make smoke MODEL_ID=...            — test the full stack with curl
#   make clean                         — remove compiled binaries
# ─────────────────────────────────────────────────────────────────────────────

COORDINATOR_ADDR ?= localhost:9090
GRPC_ADDR ?= :9090
HTTP_ADDR ?= :8080
COORDINATOR_URL ?= http://localhost:8080
MODEL ?= /path/to/model.gguf
MODEL_ID ?= phi3

.PHONY: proto build coordinator worker clean run-coordinator run-worker smoke check

## Regenerate Go bindings from proto files
proto:
	buf generate

## Build both binaries into ./bin/
build: coordinator worker

coordinator:
	go build -o bin/coordinator ./cmd/coordinator

worker:
	go build -o bin/worker ./cmd/worker

## Compile everything (includes internal packages) — good for catching errors
check:
	go build ./...
	go vet ./...

## Start coordinator (development)
run-coordinator:
	go run ./cmd/coordinator --grpc $(GRPC_ADDR) --http $(HTTP_ADDR)

## Start worker (development)
## Usage:
##   make run-worker MODEL=/path/to/model.gguf MODEL_ID=phi3
##   make run-worker MODEL=/path/to/model.gguf MODEL_ID=phi3 COORDINATOR_ADDR=192.168.1.10:9090
run-worker:
	go run ./cmd/worker \
		--id worker-1 \
		--listen :9091 \
		--llama-port 8081 \
		--model $(MODEL) \
		--model-id $(MODEL_ID) \
		--coordinator $(COORDINATOR_ADDR)

## Smoke test — requires coordinator + worker already running
## Usage:
##   make smoke MODEL_ID=phi3
##   make smoke MODEL_ID=phi3 COORDINATOR_URL=http://192.168.1.10:8080
smoke:
	curl -s $(COORDINATOR_URL)/v1/chat/completions \
		-H "Content-Type: application/json" \
		-H "X-Session-Id: smoke-test-1" \
		-d '{"model":"$(MODEL_ID)","messages":[{"role":"user","content":"Say hello in one sentence."}],"stream":true}' \
		--no-buffer

## Remove compiled output
clean:
	rm -rf bin/
