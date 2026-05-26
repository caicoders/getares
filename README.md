# Getares

Ultra-lightweight distributed AI runtime that turns idle computers into a local AI cluster for running LLMs, coding assistants and AI agents collaboratively.

## Vision

Getares transforms underutilized computers inside a local network into a distributed AI mesh capable of serving local language models, coding assistants and AI agents.

The goal is to help developers and companies reduce dependency on expensive cloud AI APIs by reusing existing hardware resources such as CPU, RAM and GPU capacity.

Instead of building another heavy cloud platform, Getares focuses on:

- ultra-low resource usage
- lightweight distributed orchestration
- local-first AI infrastructure
- developer-first workflows
- simplicity and portability
- OpenAI-compatible integrations

---

## Core Features

- Distributed AI runtime for local networks
- Lightweight mesh agents
- Automatic node discovery
- Shared CPU, RAM and GPU resource orchestration
- OpenAI-compatible API
- Local LLM inference orchestration
- VS Code integration
- Multi-user support
- Cross-platform support (Windows and Linux)

---

## Architecture

```text
VS Code / IDEs
        ↓
OpenAI-compatible API
        ↓
Gateway / Scheduler
        ↓
Distributed Mesh Agents
        ↓
Local Inference Engines (llama.cpp)
```

---

## Technical Stack

- Go
- gRPC
- protobuf
- llama.cpp
- GGUF models
- Cobra CLI

---

## Philosophy

Getares is designed to feel closer to:

- Docker
- Ollama
- lightweight infrastructure tooling

than to a traditional enterprise cloud platform.

The system prioritizes:

1. Low resource consumption
2. Simplicity
3. Fast local inference orchestration
4. Developer experience
5. Easy deployment

---

## Project Status

Early architecture and MVP development phase.

Initial MVP goals:

- Node discovery
- Distributed resource monitoring
- Remote task execution
- Local LLM inference
- OpenAI-compatible endpoints
- Multiple developers using the cluster simultaneously

---

## Long-Term Goal

Enable companies to create private distributed AI clusters using the computers they already own.

The platform aims to provide:

- Local coding assistants
- AI agents
- Internal RAG systems
- Distributed inference
- Collaborative AI infrastructure

without requiring expensive dedicated hardware or cloud-only AI providers.

---

## License

Apache License 2.0
