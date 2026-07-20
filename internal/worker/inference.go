// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	workerv1 "github.com/caicoders/getares/gen/worker/v1"
)

type LlamaServer struct {
	cmd     *exec.Cmd
	port    int
	baseUrl string // "http://127.0.0.1:8081" — Only available from this machine
}

type llamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llamaRequest struct {
	Model     string         `json:"model"`
	Messages  []llamaMessage `json:"messages"`
	Stream    bool           `json:"stream"`
	Temp      float32        `json:"temperature,omitempty"`
	MaxTokens int32          `json:"max_tokens,omitempty"`
}

// sseChunk represents the JSON for each “data: ...” line from llama-server.
// Only map the fields we need—Go ignores the rest.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"` // puntero porque puede ser null
	} `json:"choices"`
}

// StartLlamaServer launches llama-server as a subprocess and blocks until it is
// ready to receive requests or until the timeout expires.
func StartLlamaServer(modelPath string, port int) (*LlamaServer, error) {
	cmd := exec.Command(
		"llama-server",
		"--model", modelPath,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"--ctx-size", "4096",
		"-ngl", "99", // Offload all layers to the GPU if one is available; fall back to the CPU
		"--log-disable", // Reduces noise in worker logs; remove to debug the call
	)

	// Redirect the subprocess's stdout/stderr to our process.
	// Without this, the output from llama-server disappears silently.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// .Start() starts the process and returns immediately.
	// It does NOT wait for the call-server to be ready—only for the PID to exist.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Could not start llama-server: %w", err)
	}

	s := &LlamaServer{
		cmd:     cmd,
		port:    port,
		baseUrl: fmt.Sprintf("http://127.0.0.1:%d", port),
	}

	// Now we're waiting for llama-server to be fully ready.
	if err := s.waitReady(30 * time.Second); err != nil {
		// If it's not ready in 30 seconds, we'll kill the process so it doesn't become orphaned.
		cmd.Process.Kill()
		cmd.Wait() // Always call Wait() after Kill().
		return nil, fmt.Errorf("llama-server didn't respond within 30 seconds: %w", err)
	}

	// Important note about cmd.Wait():
	// After calling Kill(), you must call Wait().
	// Without it, the process remains as a zombie in the operating system—it has finished, but its entry in the process table is not released.
	// During long development sessions, zombies accumulate. Wait() “reaps” the process and frees up OS resources.

	slog.Info("llama-server ready", "port", port, "model", modelPath)
	return s, nil
}

// waitReady polls the /health endpoint on llama-server until it returns a 200 OK response or the timeout expires.
func (s *LlamaServer) waitReady(timeout time.Duration) error {
	// HTTP client with a short timeout.
	// We don't want each attempt to block for too long if the server doesn't exist yet.
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(s.baseUrl + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil // It's ready.
			}
		}
		// Will wait 500ms before the next try.
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting llama-server (%s)", timeout)
}

// Stop stops the llama-server process.
func (s *LlamaServer) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait() // Siempre harvest the process after the Kill().
	}
}

// Infer receives the gRPC request from the coordinator, converts it to the
// llama-server format, and forwards the tokens as chunks in the gRPC stream.
//
// Flow: InferRequest (proto) → JSON HTTP → SSE → InferChunk (proto stream)
func (s *LlamaServer) Infer(
	ctx context.Context,
	req *workerv1.InferRequest,
	stream workerv1.WorkerService_InferServer,
) error {
	// 1. Convert messages from the proto format to the llama-server format
	msgs := make([]llamaMessage, len(req.Messages))

	for i, m := range req.Messages {
		msgs[i] = llamaMessage{Role: m.Role, Content: m.Content}
	}

	// 2. Serialize the request to JSON
	// bytes.NewReader converts []byte into an io.Reader that http can consume
	body, err := json.Marshal(llamaRequest{
		Model:     req.ModelId,
		Messages:  msgs,
		Stream:    true, // CRITICAL: Without this, call-server waits at the end
		Temp:      req.Temperature,
		MaxTokens: req.MaxTokens,
	})

	if err != nil {
		return fmt.Errorf("Error serializing request: %w", err)
	}

	// 3. Build the HTTP request using the gRPC stream context.
	// IMPORTANT: We pass `ctx`, not `context.Background()`.
	// If the client cancels the gRPC stream, `ctx` is canceled,
	// which automatically cancels this HTTP request.

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.baseUrl+"/v1/chat/completions",
		bytes.NewReader(body),
	)

	if err != nil {
		return err
	}

	// 4. Execute the request
	resp, err := http.DefaultClient.Do(httpReq)

	if err != nil {
		return fmt.Errorf("error contacting llama-server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llama-server responded %d: %s", resp.StatusCode, b)
	}

	// 5. Parse the SSE stream and forward each token to the gRPC stream
	return parseSSE(ctx, resp.Body, stream)

}

// parseSSE reads the body of the HTTP response from llama-server line by line,
// extracts the token from each event, and sends it to the gRPC stream.
func parseSSE(
	ctx context.Context,
	r io.Reader,
	stream workerv1.WorkerService_InferServer,
) error {
	// bufio.Scanner reads from an io.Reader line by line.
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		// Check for cancellation on each iteration.
		// If the client disconnects, we stop reading immediately.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()

		// SSE lines containing data always begin with “data: ”
		// Empty lines are separators—we ignore them
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")

		// Llama-server stream end signal
		if payload == "[DONE]" {
			return stream.Send(&workerv1.InferChunk{
				Done:         true,
				FinishReason: "stop",
			})
		}

		// Deserialize the event's JSON
		var chunk sseChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // malformed line — ignore and continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Determine whether it is the last token
		done := choice.FinishReason != nil && *choice.FinishReason != ""
		finishReason := ""
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}

		// Send the token to the gRPC stream
		// stream.Send() is thread-safe; it does not require a mutex
		if err := stream.Send(&workerv1.InferChunk{
			Token:        choice.Delta.Content,
			Done:         done,
			FinishReason: finishReason,
		}); err != nil {
			return err // The gRPC client disconnected
		}

		if done {
			return nil
		}
	}

	// scanner.Err() returns nil if the stream ended normally,
	// or the error if there was a problem reading the stream.
	return scanner.Err()
}
