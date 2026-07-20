// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	workerv1 "github.com/caicoders/getares/gen/worker/v1"
	"github.com/caicoders/getares/internal/coordinator"
)

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float32       `json:"temperature"`
	MaxTokens   int32         `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
}

type modelListResponse struct {
	Object string     `json:"object"`
	Data   []struct{} `json:"data"`
}

type streamChunkResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int               `json:"index"`
		Delta        map[string]string `json:"delta"`
		FinishReason *string           `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

func NewHandler(reg *coordinator.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		handleChat(w, r, reg)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		handleModels(w, r)
	})
	return mux
}

func handleChat(w http.ResponseWriter, r *http.Request, reg *coordinator.Registry) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		modelID = "default"
	}

	client, err := reg.Pick(modelID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session_id")
	}

	inferReq := &workerv1.InferRequest{
		SessionId:   sessionID,
		ModelId:     modelID,
		Messages:    convertMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if inferReq.MaxTokens <= 0 {
		inferReq.MaxTokens = 128
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	stream, err := client.Infer(ctx, inferReq)
	if err != nil {
		slog.Error("inference stream failed", "err", err, "model", modelID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var content strings.Builder
	var finishReason string

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slog.Error("failed to receive inference chunk", "err", err)
			break
		}

		token := chunk.GetToken()
		if token != "" {
			content.WriteString(token)
			if req.Stream {
				if err := writeSSEChunk(w, modelID, token); err != nil {
					return
				}
			}
		}

		if chunk.GetDone() {
			finishReason = chunk.GetFinishReason()
			if finishReason == "" {
				finishReason = "stop"
			}
			break
		}
	}

	if req.Stream {
		finishReasonValue := finishReason
		if err := writeSSEDone(w, modelID, &finishReasonValue); err != nil {
			return
		}
		return
	}

	resp := chatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelID,
	}
	resp.Choices = append(resp.Choices, struct {
		Index        int         `json:"index"`
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	}{
		Index:        0,
		Message:      chatMessage{Role: "assistant", Content: content.String()},
		FinishReason: finishReason,
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode chat response", "err", err)
	}
}

func handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(modelListResponse{Object: "list", Data: []struct{}{}})
}

func convertMessages(messages []chatMessage) []*workerv1.Message {
	out := make([]*workerv1.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, &workerv1.Message{Role: msg.Role, Content: msg.Content})
	}
	return out
}

func appendToken(content *strings.Builder, token string) string {
	token = strings.TrimRight(token, "\r\n")
	if token == "" {
		return ""
	}

	trimmed := strings.TrimLeftFunc(token, unicode.IsSpace)
	leadingWhitespace := token[:len(token)-len(trimmed)]
	body := strings.TrimSpace(trimmed)
	if body == "" {
		content.WriteString(leadingWhitespace)
		return leadingWhitespace
	}

	if content.Len() == 0 {
		content.WriteString(leadingWhitespace + body)
		return leadingWhitespace + body
	}

	prefix := leadingWhitespace
	prev := []rune(content.String())
	last := prev[len(prev)-1]
	first := []rune(body)[0]

	if unicode.IsSpace(last) {
		content.WriteString(prefix + body)
		return prefix + body
	}

	if unicode.IsPunct(first) || unicode.IsSymbol(first) {
		content.WriteString(prefix + body)
		return prefix + body
	}

	if prefix == "" {
		switch last {
		case '.', '!', '?', ':', ';', ',', ')', ']', '}', '"', '\'', '”', '’':
			prefix = " "
		default:
			prefix = " "
		}
	}

	content.WriteString(prefix)
	content.WriteString(body)
	return prefix + body
}

func writeSSEChunk(w http.ResponseWriter, modelID, token string) error {
	payload := streamChunkResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelID,
	}
	payload.Choices = append(payload.Choices, struct {
		Index        int               `json:"index"`
		Delta        map[string]string `json:"delta"`
		FinishReason *string           `json:"finish_reason,omitempty"`
	}{
		Index: 0,
		Delta: map[string]string{"content": token},
	})

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeSSEDone(w http.ResponseWriter, modelID string, finishReason *string) error {
	payload := streamChunkResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelID,
	}
	payload.Choices = append(payload.Choices, struct {
		Index        int               `json:"index"`
		Delta        map[string]string `json:"delta"`
		FinishReason *string           `json:"finish_reason,omitempty"`
	}{
		Index:        0,
		Delta:        map[string]string{},
		FinishReason: finishReason,
	})

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "data: [DONE]"); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
