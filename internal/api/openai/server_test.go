// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caicoders/getares/internal/coordinator"
)

func TestModelsEndpointReturnsOK(t *testing.T) {
	h := NewHandler(coordinator.NewRegistry())

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func TestAppendTokenAddsSpacesBetweenWords(t *testing.T) {
	var content strings.Builder

	if got := appendToken(&content, "Hola"); got != "Hola" {
		t.Fatalf("expected first token unchanged, got %q", got)
	}
	if got := appendToken(&content, " mundo"); got != " mundo" {
		t.Fatalf("expected leading whitespace to be preserved, got %q", got)
	}
	if got := appendToken(&content, "."); got != "." {
		t.Fatalf("expected punctuation to attach without space, got %q", got)
	}
	if got := appendToken(&content, "Hola"); got != " Hola" {
		t.Fatalf("expected space after sentence punctuation, got %q", got)
	}
}
