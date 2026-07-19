<div align="center">

<!-- Replace this comment with your logo once ready: -->
<!-- <img src="assets/logo.png" alt="Getares Logo" width="200" /> -->

# Getares

**Ultra-lightweight distributed AI runtime for local networks**

Turn idle computers into a private AI cluster. Run LLMs, coding assistants and AI agents collaboratively — without cloud APIs, without data leaving your network.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/caicoders/getares?include_prereleases)](https://github.com/caicoders/getares/releases)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey)](https://github.com/caicoders/getares/releases)

[Quick Start](#-quick-start) · [How it works](#-how-it-works) · [Multi-machine setup](#-multi-machine-setup) · [VS Code integration](#-vs-code--continuedev) · [Build from source](#-building-from-source) · [Roadmap](#-roadmap)

</div>

---

## The problem

A team of 10 developers each spending $300/month on cloud AI APIs pays **$36,000/year** — sending proprietary code and sensitive data to external servers in the process.

Getares solves this by turning the hardware your team already owns into a private AI cluster: every developer's machine contributes capacity, all requests stay inside your network, and cost is measured in electricity rather than tokens.

---

## How it works

Getares has two components:

```
┌──────────────────────────────────────────────────────────────┐
│                      Your Local Network                       │
│                                                               │
│   VS Code · curl · any OpenAI-compatible tool                 │
│         │                                                     │
│         │  HTTP  OpenAI-compatible API                        │
│         ▼                                                     │
│   ┌──────────────────────────────────┐                       │
│   │           COORDINATOR            │                       │
│   │  Routes · Schedules · Registers  │                       │
│   │  Exposes API · Tracks sessions   │                       │
│   └───────────────┬──────────────────┘                       │
│                   │  gRPC                                     │
│         ┌─────────┼──────────┐                               │
│         ▼         ▼          ▼                               │
│   ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│   │ WORKER A │ │ WORKER B │ │ WORKER C │                    │
│   │ 7B model │ │13B model │ │70B model │                    │
│   │ 16GB RAM │ │ 32GB RAM │ │128GB RAM │                    │
│   └──────────┘ └──────────┘ └──────────┘                    │
└──────────────────────────────────────────────────────────────┘
```

**Coordinator** — the brain. Runs on any machine. Maintains the registry of active workers, routes each request to the best available worker based on model availability and current load, and exposes a single OpenAI-compatible HTTP endpoint to clients.

**Worker** — the muscle. Runs on machines with GPU or sufficient RAM. Loads and runs AI models using `llama-server` (llama.cpp). Each worker manages its own models independently.

> Workers hold the models. The coordinator routes to them. Clients only ever talk to the coordinator.

---

## ⚡ Quick Start

### Step 1 — Download Getares

| Platform | Binary |
|---|---|
| 🐧 Linux x86_64 | [getares-linux-amd64](https://github.com/caicoders/getares/releases/latest/download/getares-linux-amd64) |
| 🐧 Linux ARM64 | [getares-linux-arm64](https://github.com/caicoders/getares/releases/latest/download/getares-linux-arm64) |
| 🍎 macOS Apple Silicon | [getares-macos-arm64](https://github.com/caicoders/getares/releases/latest/download/getares-macos-arm64) |
| 🪟 Windows x86_64 | [getares-windows-amd64.exe](https://github.com/caicoders/getares/releases/latest/download/getares-windows-amd64.exe) |

**Linux / macOS:**
```bash
chmod +x getares
sudo mv getares /usr/local/bin/
```

**Windows:** rename to `getares.exe`, place it in a folder on your PATH, or run from the current folder with `.\getares.exe`.

---

### Step 2 — Install llama-server

Getares uses `llama-server` from [llama.cpp](https://github.com/ggerganov/llama.cpp) to run models locally.

**🐧 Arch Linux / Omarchy:**
```bash
sudo pacman -Sy llama-cpp
```

**🐧 Debian / Ubuntu:**
```bash
git clone https://github.com/ggerganov/llama.cpp
cd llama.cpp && make -j$(nproc)
sudo cp build/bin/llama-server /usr/local/bin/
```

**🍎 macOS:**
```bash
brew install llama.cpp
```

**🪟 Windows (NVIDIA GPU):**

Download the CUDA build from [llama.cpp releases](https://github.com/ggerganov/llama.cpp/releases) — look for `llama-bXXXX-bin-win-cuda-cu12.x.x-x64.zip`. Extract and add the folder to your PATH.

**Verify:**
```bash
llama-server --version
```

---

### Step 3 — Get a model

Getares requires a `.gguf` model file. Recommended options by hardware:

| Hardware | Model | Size |
|---|---|---|
| 6 GB VRAM | Llama-3.1-8B-Q4_K_M | 4.7 GB |
| 16 GB RAM (CPU) | Phi-3-mini-Q4 | 2.4 GB |
| 128 GB unified RAM | Llama-3.1-70B-Q4_K_M | 40 GB |

**Download Phi-3-mini (works on any hardware):**
```bash
mkdir -p ~/.getares/models
curl -L -o ~/.getares/models/phi3.gguf \
  "https://huggingface.co/microsoft/Phi-3-mini-4k-instruct-gguf/resolve/main/Phi-3-mini-4k-instruct-q4.gguf"
```

---

### Step 4 — Setup wizard

```bash
getares init
```

Detects your hardware, suggests compatible models, and generates `getares.yaml`.

---

### Step 5 — Start

```bash
getares start
```

---

### Step 6 — Test

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"phi3","messages":[{"role":"user","content":"Hello"}],"stream":true}' \
  --no-buffer
```

---

## 🌐 Multi-machine setup

### Topology 1 — Separate coordinator and worker

```
PC1 (coordinator)          PC2 (GPU worker)
```

**PC1:**
```bash
getares init    # choose: Coordinator
getares start
```

**PC2:**
```bash
getares init    # choose: Worker → enter PC1's IP:9090
getares start
```

**Any client on the network:**
```bash
curl http://<PC1-IP>:8080/v1/chat/completions ...
```

---

### Topology 2 — One powerful machine runs everything

```
PC2 (coordinator + worker)      PC1 (client only, no install needed)
```

**PC2:**
```bash
getares init    # choose: Both
getares start
```

**PC1:**
```bash
curl http://<PC2-IP>:8080/v1/chat/completions ...
```

---

### Windows Firewall

Run in an **Administrator PowerShell** on any Windows node:

```powershell
# Coordinator
New-NetFirewallRule -DisplayName "Getares gRPC"     -Direction Inbound -Protocol TCP -LocalPort 9090 -Action Allow
New-NetFirewallRule -DisplayName "Getares HTTP API" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow

# Worker
New-NetFirewallRule -DisplayName "Getares Worker"   -Direction Inbound -Protocol TCP -LocalPort 9091 -Action Allow
```

---

## 💬 VS Code + Continue.dev

[Continue.dev](https://continue.dev) is a free, open-source VS Code extension that adds AI chat and code suggestions powered by any OpenAI-compatible API.

**Install:** VS Code → Extensions → search **Continue** → install.

**Configure** (`~/.continue/config.yaml` on Linux/macOS, `%USERPROFILE%\.continue\config.yaml` on Windows):

```yaml
name: Getares
version: 0.0.1
schema: v1

models:
  - name: Getares — Phi-3 (fast)
    provider: openai
    model: phi3
    apiBase: http://localhost:8080/v1
    apiKey: none
    systemPrompt: "You are a helpful coding assistant. Be concise."
    defaultCompletionOptions:
      maxTokens: 512
      temperature: 0.3
    roles:
      - chat
      - autocomplete

  - name: Getares — Llama-3.1 8B (quality)
    provider: openai
    model: llama3-8b
    apiBase: http://localhost:8080/v1
    apiKey: none
    defaultCompletionOptions:
      maxTokens: 1024
      temperature: 0.5
    roles:
      - chat
```

> If the coordinator runs on another machine, replace `localhost` with its IP address.

---

## 🔧 Manual startup (without wizard)

**Coordinator:**
```bash
# Linux / macOS
getares coordinator --grpc :9090 --http :8080

# Windows
getares.exe coordinator --grpc :9090 --http :8080
```

**Worker:**
```bash
# Linux / macOS
getares worker \
  --id worker-1 \
  --model ~/.getares/models/phi3.gguf \
  --model-id phi3 \
  --coordinator <coordinator-ip>:9090

# Windows (PowerShell)
getares.exe worker `
  --id worker-1 `
  --model "$env:USERPROFILE\.getares\models\phi3.gguf" `
  --model-id phi3 `
  --coordinator <coordinator-ip>:9090
```

---

## 🛠 Building from source

Requires: Go 1.22+, [buf](https://buf.build/docs/installation), `llama-server` on PATH.

```bash
git clone https://github.com/caicoders/getares
cd getares
go mod tidy
buf generate
go build -o getares ./cmd/getares
```

**Cross-compile:**
```bash
# Windows binary
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o getares.exe ./cmd/getares

# macOS ARM binary
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o getares ./cmd/getares
```

---

## 📋 Requirements

| Requirement | Notes |
|---|---|
| `llama-server` on PATH | See Step 2 |
| A `.gguf` model file | `getares init` shows download instructions |
| Go 1.22+ | Only required to build from source |
| NVIDIA drivers 525+ | Only for NVIDIA GPU inference |
| CUDA Toolkit 12.x | Only for NVIDIA GPU on Linux |

---

## ⚠️ Known limitations (v0.1.0-alpha)

- No TLS — use on trusted LANs only. Do not expose ports to the internet.
- No authentication — any device on the LAN can send requests.
- Single coordinator — no HA yet. Restart is sub-second and workers reconnect automatically.
- No automatic model download — `getares init` shows the download command.
- mDNS auto-discovery not yet active — workers must specify the coordinator address.

---

## 🗺 Roadmap

| Version | Focus |
|---|---|
| **v0.1.0-alpha** | ✅ Core routing, OpenAI API, multi-machine support, setup wizard |
| v0.2.0-alpha | mDNS zero-config discovery, session affinity, tiered routing |
| v0.3.0-alpha | VRAM-aware scheduling, model load/unload lifecycle |
| v0.1.0 stable | TLS, API key auth, crash recovery, packaged installers |

---

## 🤝 Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a PR.

All PRs target the `develop` branch. `main` only receives tagged releases.

---

## Philosophy

Getares is designed to feel like Docker or Ollama — not like a cloud platform. The system prioritizes:

1. Ultra-low resource consumption
2. Operational simplicity
3. Developer experience
4. Modularity
5. Local-first architecture

---

## License

[GNU Affero General Public License v3.0](LICENSE) — free to use, modify and share. Derivative works and hosted services must remain open source under the same license.

For commercial licensing, contact [caicoders](https://github.com/caicoders).

---

<div align="center">
<sub>Built with Go · llama.cpp · gRPC · protobuf · OpenAI-compatible API</sub><br/>
<sub>A <a href="https://github.com/caicoders">caicoders</a> project</sub>
</div>
