<div align="center">

# Getares

**Ultra-lightweight distributed AI runtime for local networks**

Turn idle computers into a private AI cluster. Run LLMs, coding assistants and AI agents collaboratively — without cloud APIs, without data leaving your network.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/caicoders/getares?include_prereleases)](https://github.com/caicoders/getares/releases)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey)](https://github.com/caicoders/getares/releases)

[Are you a user or an admin?](#are-you-a-user-or-an-admin) · [Quick Start](#quick-start-admin) · [Using Getares as a client](#using-getares-as-a-client) · [How it works](#how-it-works) · [Multi-machine setup](#multi-machine-setup) · [Build from source](#building-from-source) · [Roadmap](#roadmap)

</div>

---

## The problem

A team of 10 developers each spending $300/month on cloud AI APIs pays **$36,000/year** — sending proprietary code and sensitive data to external servers in the process.

Getares solves this by turning the hardware your team already owns into a private AI cluster: every developer's machine contributes capacity, all requests stay inside your network, and cost is measured in electricity rather than tokens.

---

## Are you a user or an admin?

Getares has two kinds of people:

**Admin / Tech Lead** — installs and configures Getares on the team's machines. Sets up the coordinator and workers. Does this once.
→ Follow the [Quick Start (Admin)](#quick-start-admin) section below.

**Developer / User** — already has a Getares coordinator running on the network (set up by someone else). Just wants to use AI in VS Code or from the terminal.
→ Jump directly to [Using Getares as a client](#using-getares-as-a-client). You don't need to install anything.

---

## Using Getares as a client

> **This section is for developers whose team already has Getares running.**
> You don't need to install Getares or know anything about coordinators or workers.
> Just ask your admin for the coordinator URL and the model name.

You need two things from whoever set up Getares on your team:
- The **coordinator URL** (example: `http://192.168.1.10:8080`)
- The **model name** (example: `phi3`, `qwen25`, `llama3`)

---

### Option A — Use it in VS Code with Continue.dev (recommended)

Continue.dev adds an AI sidebar and inline code suggestions to VS Code, powered by Getares running on your local network.

**Step 1 — Install Continue.dev**

Open VS Code → click the Extensions icon (or press `Ctrl+Shift+X`) → search **Continue** → click Install.

Once installed, a new icon appears in the left sidebar that looks like a speech bubble. Click it to open the AI chat panel.

**Step 2 — Connect Continue.dev to Getares**

Open the Continue config file:
- **Linux / macOS:** `~/.continue/config.yaml`
- **Windows:** `C:\Users\YourName\.continue\config.yaml`

If the file does not exist, create it. Paste this content and replace the values your admin gave you:

```yaml
name: Getares
version: 1.0.0
schema: v1

models:
  - name: Getares AI
    provider: openai
    model: YOUR_MODEL_NAME_HERE       # e.g. phi3, qwen25, llama3
    apiBase: http://YOUR_COORDINATOR_IP:8080/v1   # e.g. http://192.168.1.10:8080/v1
    apiKey: none
    systemPrompt: "You are an expert coding assistant. Be concise and precise."
    defaultCompletionOptions:
      maxTokens: 2048
      temperature: 0.1
      contextLength: 32000
    roles:
      - chat
      - edit
      - apply
      - autocomplete
```

Save the file. VS Code picks up the change automatically — no restart needed.

**Step 3 — Start using it**

- **Chat:** click the Continue icon in the sidebar → type your question → press Enter.
- **Explain code:** select any code → right-click → **Continue: Explain Code**.
- **Fix an error:** select the error message in the terminal → right-click → **Continue: Fix Code**.
- **Autocomplete:** just start typing in any file — suggestions appear automatically as you type.

**Quick test:** open the Continue sidebar and type:

```
Write a Go function that checks if a string is a palindrome.
```

You should see a response start streaming within a few seconds. If nothing happens after 10 seconds, check the troubleshooting section below.

---

### Option B — Use it from the terminal with curl

No installation required. Replace the values with what your admin gave you:

```bash
curl -s http://YOUR_COORDINATOR_IP:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_MODEL_NAME",
    "messages": [{"role": "user", "content": "Explain what a goroutine is in one paragraph."}],
    "stream": true
  }' \
  --no-buffer
```

Tokens will stream to your terminal as they are generated. To get a single JSON response instead of streaming, change `"stream": true` to `"stream": false`.

---

### Option C — Use it from any OpenAI-compatible tool

Getares exposes a standard OpenAI-compatible API. Any tool that lets you configure a custom OpenAI endpoint works with Getares out of the box.

| Tool | Where to configure |
|---|---|
| Continue.dev | `apiBase` in config.yaml (see Option A) |
| Cursor | Settings → Models → OpenAI → Base URL |
| Aider | `--openai-api-base` flag |
| LangChain | `openai_api_base` parameter |
| Any HTTP client | `POST http://<coordinator>:8080/v1/chat/completions` |

The API key field can be set to any value (e.g. `none`) — Getares does not validate it in this version.

---

### Troubleshooting (client side)

**Continue.dev shows no response / times out**

1. Verify the coordinator is reachable:
   ```bash
   curl http://YOUR_COORDINATOR_IP:8080/v1/models
   ```
   If this returns an error, the coordinator is not reachable from your machine. Ask your admin to check that port 8080 is open.

2. Verify the model name matches exactly what the admin configured:
   ```bash
   # The model name in config.yaml must match exactly what the worker registered with
   # Ask your admin: what --model-id did you use when starting the worker?
   ```

3. Check that `apiBase` ends in `/v1` — not `/v1/chat/completions`:
   ```yaml
   apiBase: http://192.168.1.10:8080/v1       # ✅ correct
   apiBase: http://192.168.1.10:8080/v1/chat/completions  # ❌ wrong
   ```

4. Reload VS Code after editing config.yaml: press `Ctrl+Shift+P` → type **Developer: Reload Window** → Enter.

**Responses are slow**

This depends on the hardware of the worker running the model. A 7B model on CPU takes 5-20 seconds to start responding. A 7B model on a GPU typically responds in under 2 seconds. Ask your admin which hardware is running the model you're using.

**The model says it is Claude or ChatGPT**

The model is running correctly — it just doesn't know its own name because no system prompt tells it. Add a `systemPrompt` to your config.yaml:

```yaml
systemPrompt: "You are a coding assistant powered by Getares running on our local network."
```

---

## Quick Start (Admin)

> **This section is for the person setting up Getares on the team's machines.**

### Step 1 — Download Getares

| Platform | Binary |
|---|---|
| 🐧 Linux x86_64 | [getares-linux-amd64](https://github.com/caicoders/getares/releases/download/v0.1.0-alpha/getares-linux-amd64) |
| 🐧 Linux ARM64 | [getares-linux-arm64](https://github.com/caicoders/getares/releases/download/v0.1.0-alpha/getares-linux-arm64) |
| 🍎 macOS Apple Silicon | [getares-macos-arm64](https://github.com/caicoders/getares/releases/download/v0.1.0-alpha/getares-macos-arm64) |
| 🪟 Windows x86_64 | [getares-windows-amd64.exe](https://github.com/caicoders/getares/releases/download/v0.1.0-alpha/getares-windows-amd64.exe) |

**Linux / macOS:**
```bash
chmod +x getares
sudo mv getares /usr/local/bin/
```

**Windows:** rename to `getares.exe`, place it in a folder on your PATH, or run from the current folder with `.\getares.exe`.

---

### Step 2 — Install llama-server

Getares uses `llama-server` from [llama.cpp](https://github.com/ggerganov/llama.cpp) to run models locally.

**🐧 Arch Linux:**
```bash
sudo pacman -Sy llama-cpp
```

**🐧 Fedora:**
```bash
sudo dnf install -y llama-cpp
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

Getares requires a `.gguf` model file. A good place to compare which models fit your hardware is [canirun.ai](https://www.canirun.ai/), which shows which models run well based on your CPU/GPU/RAM specs.

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

### Step 7 — Tell your team

Once Getares is running, share these two things with your team:

```
Coordinator URL: http://<YOUR-IP>:8080
Model name:      <the model-id you configured>

They only need to follow the "Using Getares as a client" section in the README.
No installation required on their side.
```

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

## Current worker-selection rules

At the moment, routing is intentionally simple:

1. The client sends the request to the coordinator.
2. The coordinator looks at its registered workers.
3. It prefers a worker that already has the requested model loaded.
4. If no worker has that model loaded, it falls back to the first available worker in the registry.
5. The request is then forwarded to that worker over gRPC.

There is no round-robin, no advanced load balancing, and no intelligent scheduling yet. If multiple workers can serve the same model, the coordinator will pick one from the current registry state rather than using a full balancing policy.

---

## Multi-machine setup

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

**PC1 — no installation needed:**
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

## Manual startup (without wizard)

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

## Building from source

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

## Requirements

| Requirement | Notes |
|---|---|
| `llama-server` on PATH | See Step 2 — admin only |
| A `.gguf` model file | `getares init` shows download instructions — admin only |
| Go 1.22+ | Only required to build from source |
| NVIDIA drivers 525+ | Only for NVIDIA GPU inference |
| CUDA Toolkit 12.x | Only for NVIDIA GPU on Linux |

---

## Known limitations (v0.1.0-alpha)

- No TLS — use on trusted LANs only. Do not expose ports to the internet.
- No authentication — any device on the LAN can send requests.
- Single coordinator — no HA yet. Restart is sub-second and workers reconnect automatically.
- No automatic model download — `getares init` shows the download command.
- mDNS auto-discovery not yet active — workers must specify the coordinator address manually.

---

## FAQ

### I'm a developer on the team. Do I need to install Getares?
No. You only need VS Code with the Continue.dev extension, and the coordinator URL from your admin. See [Using Getares as a client](#-using-getares-as-a-client).

### How is the worker chosen for a request?
The coordinator picks a registered worker that has the requested model loaded. If none does, it falls back to the first available worker. There is no round-robin or advanced load balancing yet — that comes in v0.2.0.

### Do workers talk to each other directly?
No. Each worker communicates only with the coordinator. The coordinator forwards requests to one worker over gRPC. Workers are unaware of each other.

### What happens if a worker goes down?
The coordinator detects missed heartbeats and removes the worker from the registry after 15 seconds. Subsequent requests are routed to the remaining active workers.

### Can Getares run without a GPU?
Yes. Models run on CPU if no GPU is available. Performance will be significantly lower — expect 5-30 tokens per second instead of 30-150+ on a GPU.

### What if two workers have the same model?
The coordinator picks one based on its registry state. Full load-aware scheduling (choosing the least busy worker) is planned for v0.2.0.

### Is my data sent to the internet?
No. Getares routes all traffic within your local network. The models run on your hardware. No data leaves your network.

---

## Roadmap

| Version | Focus |
|---|---|
| **v0.1.0-alpha** | ✅ Core routing, OpenAI API, multi-machine support, setup wizard |
| v0.2.0-alpha | mDNS zero-config discovery, session affinity, tiered routing |
| v0.3.0-alpha | VRAM-aware scheduling, model load/unload lifecycle |
| v0.1.0 stable | TLS, API key auth, crash recovery, packaged installers |

---

## Contributing

Contributions are not yet open for this project. For now, there is no public contribution workflow or PR process in place, so please do not open pull requests yet.

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
