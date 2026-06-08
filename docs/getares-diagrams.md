# Getares — Visual Architecture Reference

> **How to render these diagrams**
> - **GitHub:** Push this file as any `.md` file. GitHub renders Mermaid natively.
> - **VS Code:** Install the extension [Markdown Preview Mermaid Support](https://marketplace.visualstudio.com/items?itemName=bierner.markdown-mermaid), then open Preview (`Cmd+Shift+V` / `Ctrl+Shift+V`).
> - **Standalone:** Paste any block into [mermaid.live](https://mermaid.live) to render and export as SVG/PNG.

---

## Diagram 1 — General Architecture View (The LAN Ecosystem)

This is the **10,000-foot view** of the entire system. Read it top-to-bottom: clients at the top, infrastructure at the bottom.

```mermaid
graph TD
    subgraph CLIENTS["🖥️  Clients (outside the runtime)"]
        C1["curl / HTTP client"]
        C2["VS Code + Continue.dev"]
        C3["Any OpenAI-compatible tool"]
    end

    subgraph COORDINATOR["⚙️  Coordinator Process  (single Go binary)"]
        direction TB
        HTTP["HTTP Server\n:8080\nnet/http stdlib\n──────────────\nPOST /v1/chat/completions\nGET  /v1/models"]
        GRPC_C["gRPC Server\n:9090\n──────────────\nRegister()\nHeartbeat()"]
        REG["In-Memory Registry\nsync.RWMutex\n──────────────\nmap[nodeID → Node]\nTTL eviction goroutine\ngRPC client per worker"]
        SCHED["Scheduler / Router\n──────────────\nPick(modelID) → client\nSession map (Sprint 2)\nScoring fn (Sprint 3)"]

        HTTP --> SCHED
        SCHED --> REG
        GRPC_C --> REG
    end

    subgraph LAN["🌐  Local Area Network"]
        subgraph W1["Worker Node A  (Linux · GPU)"]
            WS1["gRPC Server :9091\n──────────────\nInfer()  Health()"]
            LS1["llama-server :8081\nos/exec subprocess\n──────────────\nPOST /v1/chat/completions\nSSE streaming"]
            WS1 -->|"HTTP + SSE\nlocalhost only"| LS1
        end

        subgraph W2["Worker Node B  (Windows · CPU)"]
            WS2["gRPC Server :9092\n──────────────\nInfer()  Health()"]
            LS2["llama-server :8082\nos/exec subprocess\n──────────────\nPOST /v1/chat/completions\nSSE streaming"]
            WS2 -->|"HTTP + SSE\nlocalhost only"| LS2
        end

        subgraph W3["Worker Node C  (macOS · Apple Silicon)"]
            WS3["gRPC Server :9093\n──────────────\nInfer()  Health()"]
            LS3["llama-server :8083\nos/exec subprocess\n──────────────\nPOST /v1/chat/completions\nSSE streaming"]
            WS3 -->|"HTTP + SSE\nlocalhost only"| LS3
        end
    end

    C1 -->|"HTTP/1.1\nSSE stream"| HTTP
    C2 -->|"HTTP/1.1\nSSE stream"| HTTP
    C3 -->|"HTTP/1.1\nSSE stream"| HTTP

    REG -->|"gRPC stream\nInfer()"| WS1
    REG -->|"gRPC stream\nInfer()"| WS2
    REG -->|"gRPC stream\nInfer()"| WS3

    WS1 -->|"Register()\nHeartbeat() every 5s"| GRPC_C
    WS2 -->|"Register()\nHeartbeat() every 5s"| GRPC_C
    WS3 -->|"Register()\nHeartbeat() every 5s"| GRPC_C

    style CLIENTS fill:#1e293b,stroke:#475569,color:#e2e8f0
    style COORDINATOR fill:#1e3a5f,stroke:#3b82f6,color:#e2e8f0
    style LAN fill:#1a2e1a,stroke:#4ade80,color:#e2e8f0
    style W1 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style W2 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style W3 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style HTTP fill:#1e40af,stroke:#60a5fa,color:#e2e8f0
    style GRPC_C fill:#1e40af,stroke:#60a5fa,color:#e2e8f0
    style REG fill:#4c1d95,stroke:#a78bfa,color:#e2e8f0
    style SCHED fill:#4c1d95,stroke:#a78bfa,color:#e2e8f0
```

### How to read it — Java → Go translation

**The Coordinator is not a class, it is a process.** In Java you might model this as a service with injected dependencies. In Go, it is a binary that wires its components in `main.go` and passes them by pointer. `Registry` is the shared state — everything else holds a reference to it.

**`sync.RWMutex` is your `ReadWriteLock`.** The Registry uses it to protect the `map[string]*Node`. Multiple goroutines can read concurrently (`RLock`), but writing (registering or evicting a node) requires exclusive access (`Lock`). Forgetting to unlock is the Go equivalent of a deadlock — always use `defer mu.Unlock()` immediately after `mu.Lock()`.

**The gRPC client lives inside the Registry, not in the worker.** When a worker registers, the coordinator opens a gRPC connection *to the worker* and stores it in `Node.Client`. This is the connection the coordinator uses later when it calls `Infer()`. The worker does not call back — the coordinator calls forward.

**Critical edge case — eviction race:** The eviction goroutine runs every 5 seconds and calls `Lock()`. If a request is in-flight using `Node.Client` at the same moment eviction tries to `delete(r.nodes, id)` and `conn.Close()`, you have a race condition. Sprint 1 ignores this (acceptable). Sprint 4 fixes it by reference-counting active streams before closing.

---

## Diagram 2 — Request Lifecycle Flow (Sprint 1 — The Vertical Slice)

This is a **sequence diagram**: time flows top-to-bottom, and each vertical bar is an active participant. An arrow is a function call or network message.

```mermaid
sequenceDiagram
    actor Client as 🖥️ Client<br/>(curl / IDE)
    participant HTTP as HTTP Handler<br/>:8080
    participant SCHED as Scheduler<br/>(Registry.Pick)
    participant GRPC_W as gRPC Channel<br/>(to Worker)
    participant WS as Worker gRPC Server<br/>:9091
    participant LLAMA as llama-server<br/>:8081

    Note over Client,LLAMA: ── Sprint 1 happy path — single worker, no session affinity ──

    Client->>+HTTP: POST /v1/chat/completions<br/>{"model":"phi3","stream":true,...}
    HTTP->>HTTP: json.Decode(body) → chatRequest{}

    HTTP->>+SCHED: reg.Pick("phi3")
    Note right of SCHED: Iterates map[nodeID→Node]<br/>Returns first Node with model "phi3"
    SCHED-->>-HTTP: workerv1.WorkerServiceClient

    HTTP->>HTTP: Set headers:<br/>Content-Type: text/event-stream<br/>Cache-Control: no-cache

    HTTP->>+GRPC_W: client.Infer(ctx, InferRequest{...})
    Note right of GRPC_W: Opens a gRPC server-streaming RPC.<br/>Returns a stream handle immediately.<br/>Tokens arrive via stream.Recv()

    GRPC_W->>+WS: [gRPC stream opens]<br/>InferRequest{session_id, model_id, messages}

    WS->>WS: llama.Infer(ctx, req, stream)

    WS->>+LLAMA: POST http://127.0.0.1:8081/v1/chat/completions<br/>{"stream":true, "messages":[...]}
    Note right of LLAMA: llama-server starts generating tokens.<br/>Each token is a Server-Sent Event (SSE).

    loop For each generated token
        LLAMA-->>WS: data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n
        WS->>WS: parseSSE() → token string
        WS-->>GRPC_W: stream.Send(InferChunk{token:"Hello", done:false})
        GRPC_W-->>HTTP: stream.Recv() → InferChunk
        HTTP->>HTTP: json.Marshal(sseChunk{...})
        HTTP-->>Client: data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n
        HTTP->>HTTP: flusher.Flush()
    end

    LLAMA-->>WS: data: [DONE]\n\n
    WS-->>GRPC_W: stream.Send(InferChunk{done:true, finish_reason:"stop"})
    GRPC_W-->>HTTP: stream.Recv() → InferChunk{done:true}
    HTTP-->>Client: data: [DONE]\n\n
    HTTP-->>-Client: [HTTP response ends]

    deactivate GRPC_W
    deactivate WS
    deactivate LLAMA

    Note over Client,LLAMA: ── Error path — no worker available ──

    Client->>HTTP: POST /v1/chat/completions {"model":"unknown"}
    HTTP->>SCHED: reg.Pick("unknown")
    SCHED-->>HTTP: error: "no workers available for model unknown"
    HTTP-->>Client: HTTP 503 Service Unavailable
```

### How to read it — Java → Go translation

**`client.Infer()` does not block until all tokens arrive.** This is the key mental shift. In Java you might call a method and get a result. Here, `client.Infer()` returns a *stream handle* immediately. You then loop calling `stream.Recv()` — each call blocks until the next token arrives. This is like Java's `Iterator.next()` but over a network.

**`flusher.Flush()` is mandatory for SSE.** Go's `http.ResponseWriter` buffers writes by default. Without `Flush()` after each token, the client sees nothing until the entire response is buffered — which defeats streaming. Always type-assert to `http.Flusher` and call it after every `fmt.Fprintf`.

**`context.Context` is the kill switch for the whole chain.** When the client disconnects mid-stream, Go's HTTP server cancels the request context. This cancellation propagates through `client.Infer(ctx, ...)` to the gRPC channel, which cancels the stream to the worker, which cancels the HTTP request to llama-server. The entire chain cleans up automatically — *only if you pass the context through every call*. Never ignore the `ctx` parameter.

**Critical edge case — `stream.Recv()` returns `io.EOF` on clean end, not `nil`.** The loop termination condition is:
```go
chunk, err := stream.Recv()
if err == io.EOF { break }         // ← normal end
if err != nil { return err }       // ← real error
```
If you treat `io.EOF` as an error, you break the streaming response for every successful request.

---

## Diagram 3 — Network Topology and Discovery (Sprint 2 — mDNS)

This diagram shows **what happens on the LAN** when mDNS is active. It has two phases: discovery (one-time, on startup) and operation (ongoing).

```mermaid
graph LR
    subgraph LAN_NET["🌐  LAN — 192.168.1.0/24  (example)"]
        direction TB

        subgraph COORD_HOST["Host: 192.168.1.10"]
            COORD["Coordinator\n:9090 gRPC\n:8080 HTTP"]
            BROWSE["mDNS Browser\nhashicorp/mdns\n─────────────\nListens for\n_getares._tcp"]
            COORD --- BROWSE
        end

        subgraph W_HOST1["Host: 192.168.1.20  (Linux GPU)"]
            W1["Worker A\n:9091 gRPC"]
            MDNS1["mDNS Announcer\n─────────────\nBroadcasts:\n_getares._tcp\nname=worker-a\nport=9091"]
            W1 --- MDNS1
        end

        subgraph W_HOST2["Host: 192.168.1.30  (Windows CPU)"]
            W2["Worker B\n:9092 gRPC"]
            MDNS2["mDNS Announcer\n─────────────\nBroadcasts:\n_getares._tcp\nname=worker-b\nport=9092"]
            W2 --- MDNS2
        end

        subgraph W_HOST3["Host: 192.168.1.40  (macOS)"]
            W3["Worker C\n:9093 gRPC"]
            MDNS3["mDNS Announcer\n─────────────\nBroadcasts:\n_getares._tcp\nname=worker-c\nport=9093"]
            W3 --- MDNS3
        end

        MDNS1 -->|"① UDP multicast\n224.0.0.251:5353\n'I am worker-a at .20:9091'"| BROWSE
        MDNS2 -->|"① UDP multicast\n224.0.0.251:5353\n'I am worker-b at .30:9092'"| BROWSE
        MDNS3 -->|"① UDP multicast\n224.0.0.251:5353\n'I am worker-c at .40:9093'"| BROWSE

        BROWSE -->|"② Auto-Register\ncoordinator.Register()\ngRPC call"| W1
        BROWSE -->|"② Auto-Register\ncoordinator.Register()\ngRPC call"| W2
        BROWSE -->|"② Auto-Register\ncoordinator.Register()\ngRPC call"| W3

        W1 -->|"③ Heartbeat every 5s\ncoordinator.Heartbeat()"| COORD
        W2 -->|"③ Heartbeat every 5s\ncoordinator.Heartbeat()"| COORD
        W3 -->|"③ Heartbeat every 5s\ncoordinator.Heartbeat()"| COORD
    end

    subgraph LEGEND["Legend"]
        direction LR
        L1["① mDNS = zero-config discovery\n   No manual --coordinator flag needed"]
        L2["② Auto-register = coordinator connects\n   to the worker and stores the gRPC client"]
        L3["③ Heartbeat = TTL refresh\n   Missing 3× → eviction from registry"]
    end

    style LAN_NET fill:#0f172a,stroke:#334155,color:#e2e8f0
    style COORD_HOST fill:#1e3a5f,stroke:#3b82f6,color:#e2e8f0
    style W_HOST1 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style W_HOST2 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style W_HOST3 fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style LEGEND fill:#1e1e2e,stroke:#45475a,color:#cdd6f4
```

### How to read it — Java → Go translation

**mDNS is UDP multicast, not TCP.** Workers broadcast to the special address `224.0.0.251` on port `5353`. Every device on the LAN receives this — there is no server to configure. The coordinator's mDNS browser receives these broadcasts and automatically calls `Register()`. Think of it as a pub/sub bus built into every LAN.

**`hashicorp/mdns` handles the multicast plumbing.** You do not write UDP code. You call `mdns.Register(service)` on the worker side and `mdns.Lookup("_getares._tcp", entriesCh)` on the coordinator side. The library fires entries into a Go channel (`chan *mdns.ServiceEntry`) as workers appear.

**mDNS is complementary to heartbeats, not a replacement.** mDNS tells the coordinator "this worker exists." Heartbeats tell it "this worker is still alive." mDNS discovery is a one-time event per worker startup. Heartbeats run every 5 seconds forever. If a worker crashes silently (no graceful shutdown), mDNS never fires a "goodbye" — the coordinator only knows about the crash when heartbeats stop and TTL expires.

**Critical edge case — mDNS does not work across subnets.** UDP multicast is limited to the local subnet. If your team has machines on different VLANs or subnets, mDNS will not reach them. The fallback is the original `--coordinator` flag (manual registration), which continues to work alongside mDNS. Both paths converge at `Registry.Add()`.

---

## Diagram 4 — Worker Process Internal Structure (Concurrency Map)

This diagram maps the **concurrency model inside one Worker process**. Each box is a goroutine. Arrows show how they communicate.

```mermaid
flowchart TD
    subgraph PROC["⚙️  Worker OS Process  (go run ./cmd/worker)"]
        direction TB

        MAIN["🔵 Main Goroutine\nmain() in cmd/worker/main.go\n─────────────────────────────\n1. Parse CLI flags\n2. StartLlamaServer() → blocks until /health OK\n3. net.Listen() → open TCP :9091\n4. grpc.NewServer() + RegisterWorkerServiceServer()\n5. go srv.Serve(lis)  ← spawns gRPC goroutines\n6. registerLoop()    ← blocks here forever"]

        subgraph GRPC_POOL["gRPC Goroutine Pool  (managed by grpc.Server)"]
            direction LR
            G1["Goroutine\nper active\nInfer() stream"]
            G2["Goroutine\nper active\nInfer() stream"]
            G3["Goroutine\nHealth()\nrequest"]
        end

        HB["🟡 Heartbeat Goroutine\nregisterLoop() inside main goroutine\n─────────────────────────────\nfor range time.Tick(5s):\n  client.Heartbeat(ctx, req)"]

        subgraph SUBPROCESS["Native Subprocess  (not a goroutine — separate OS process)"]
            LLAMA_P["llama-server\nmanaged by os/exec.Cmd\n─────────────────────────────\nCmd.Start()  → spawns PID\nCmd.Wait()   → reap on exit\nCmd.Process.Kill() → terminate\n\nExposes HTTP on 127.0.0.1:808X\nOnly reachable from this host"]
        end

        EVICT["🟠 Eviction not here\n(runs in Coordinator, not Worker)"]
    end

    MAIN -->|"go srv.Serve(lis)\nnon-blocking, spawns goroutines\non each incoming RPC"| GRPC_POOL
    MAIN -->|"os/exec.Command(...).Start()\nCmd stored in LlamaServer struct"| SUBPROCESS
    MAIN -->|"registerLoop() blocks in\nmain goroutine after setup"| HB

    G1 -->|"http.Post to\n127.0.0.1:8081\n+ parse SSE"| SUBPROCESS
    G2 -->|"http.Post to\n127.0.0.1:8081\n+ parse SSE"| SUBPROCESS
    G3 -.->|"reads nodeID,\nmodelID (no mutex\nneeded — read-only\nafter init)"| MAIN

    subgraph CHANNELS["Go Communication Patterns used in this process"]
        direction LR
        CH1["time.Tick(5s)\nchan ← time.Time\nheartbeat trigger"]
        CH2["stream.Send()\n/ stream.Recv()\ngRPC internal channels\n(hidden from you)"]
        CH3["context.Done()\nchan struct{}\ncancellation signal"]
    end

    HB --- CH1
    GRPC_POOL --- CH2
    GRPC_POOL --- CH3

    style PROC fill:#0f172a,stroke:#334155,color:#e2e8f0
    style MAIN fill:#1e3a5f,stroke:#60a5fa,color:#e2e8f0
    style GRPC_POOL fill:#1a1a2e,stroke:#7c3aed,color:#e2e8f0
    style G1 fill:#4c1d95,stroke:#a78bfa,color:#e2e8f0
    style G2 fill:#4c1d95,stroke:#a78bfa,color:#e2e8f0
    style G3 fill:#4c1d95,stroke:#a78bfa,color:#e2e8f0
    style HB fill:#713f12,stroke:#fbbf24,color:#e2e8f0
    style SUBPROCESS fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style LLAMA_P fill:#14532d,stroke:#4ade80,color:#e2e8f0
    style CHANNELS fill:#1e1e1e,stroke:#555,color:#aaa
    style EVICT fill:#1a1a1a,stroke:#444,color:#666
```

### How to read it — Java → Go translation

**`go srv.Serve(lis)` creates a goroutine *pool*, not a single thread.** When you write `go srv.Serve(lis)`, you launch the gRPC server in a goroutine. The gRPC library internally creates one new goroutine per incoming RPC call. If 10 clients call `Infer()` simultaneously, there are 10 goroutines running concurrently. This is unlike Java's thread pools where you configure size — Go goroutines are ~2KB stack, and the runtime manages them. You can have thousands.

**`llama-server` is not a goroutine — it is a separate OS process.** This is the most important distinction in the Worker. `exec.Command(...).Start()` forks a real OS process with its own PID. It is not concurrent Go code — it is concurrent at the OS level. Go communicates with it over HTTP (localhost only). `cmd.Process.Kill()` sends `SIGKILL`. `defer llama.Stop()` in `main.go` ensures it is killed when the worker exits — without this, orphaned `llama-server` processes accumulate every time you restart the worker during development.

**The main goroutine blocks in `registerLoop()` by design.** In Java you might put the heartbeat in a `@Scheduled` method and let the main thread return. In Go, if `main()` returns, the entire program exits — all goroutines are killed. So the main goroutine must block on something meaningful. `registerLoop()` loops forever on `time.Tick(5s)`, which is a perfect blocking point. The gRPC server runs independently in its goroutine.

**`context.Done()` is your thread-interrupt equivalent.** In Java, `Thread.interrupt()` signals a thread to stop. In Go, you cancel a `context.Context`. When the client disconnects during an `Infer()` stream, Go's HTTP server cancels the request context. This signals cancellation through the gRPC stream's context, which unblocks `stream.Recv()` in the coordinator and cancels the HTTP request to llama-server. The entire chain unwinds — but only if every function in the chain accepts and forwards `ctx`.

**Critical edge case — calling `cmd.Wait()` is not optional.** After `cmd.Process.Kill()`, you must call `cmd.Wait()` to reap the process. If you do not, you create a zombie process (a process that has exited but whose entry in the OS process table has not been freed). In long-running development sessions, zombie llama-server processes can exhaust OS limits. The `Stop()` method in `LlamaServer` handles this correctly — never kill the process without waiting for it.

---

## Quick Reference — Go Concurrency Patterns in this project

| Pattern | Where used in Getares | Java equivalent |
|---|---|---|
| `go func()` | Launch gRPC server, eviction loop | `new Thread(...).start()` |
| `sync.RWMutex` | Registry node map | `ReadWriteLock` |
| `time.Tick(d)` | Heartbeat loop | `ScheduledExecutorService` |
| `context.Context` | Cancel chains on disconnect | `Thread.interrupt()` |
| `defer` | Cleanup llama-server, unlock mutex | `finally` block |
| `chan T` | gRPC stream internals, mDNS entries | `BlockingQueue<T>` |
| `os/exec.Cmd` | llama-server subprocess | `ProcessBuilder` |
| `io.EOF` | End of gRPC stream | `StopIteration` / `hasNext()` == false |
| `http.Flusher` | SSE streaming | `response.flushBuffer()` in Servlet |
