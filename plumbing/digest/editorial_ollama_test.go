package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaClientChatJSON_WritesTraceFile(t *testing.T) {
	var captured ollamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/chat", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		require.NotNil(t, captured.Think)
		assert.False(t, *captured.Think)

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{
			"message": {
				"content": "{\"assignments\":[{\"rkey\":\"abc123\",\"section_id\":\"tech\"}]}"
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	traceDir := t.TempDir()
	client := &OllamaClient{
		host:       server.URL,
		model:      "test-model",
		httpClient: server.Client(),
		traceDir:   traceDir,
	}

	var out EditorialCategorization
	err := client.ChatJSON(
		context.Background(),
		"categorize batch 0 10",
		"system prompt",
		"user prompt",
		categorizationSchema(),
		&out,
	)
	require.NoError(t, err)
	require.Len(t, out.Assignments, 1)
	assert.Equal(t, "abc123", out.Assignments[0].Rkey)

	entries, err := os.ReadDir(traceDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	tracePath := filepath.Join(traceDir, entries[0].Name())
	data, err := os.ReadFile(tracePath)
	require.NoError(t, err)

	var trace OllamaTraceRecord
	require.NoError(t, json.Unmarshal(data, &trace))
	assert.Equal(t, "categorize batch 0 10", trace.Label)
	assert.Equal(t, "test-model", trace.Model)
	assert.Equal(t, 200, trace.HTTPStatus)
	assert.Contains(t, trace.ResponseBody, "assignments")
	assert.Contains(t, trace.NormalizedJSON, "abc123")
	assert.NotEmpty(t, trace.ParsedResponse)
}

func TestOllamaClientChatJSON_SendsBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "alice", username)
		assert.Equal(t, "secret", password)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"message": {
				"content": "{\"assignments\":[{\"rkey\":\"abc123\",\"section_id\":\"tech\"}]}"
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := &OllamaClient{
		host:       server.URL,
		model:      "test-model",
		username:   "alice",
		password:   "secret",
		httpClient: server.Client(),
		traceDir:   t.TempDir(),
	}

	var out EditorialCategorization
	err := client.ChatJSON(
		context.Background(),
		"categorize batch 0 10",
		"system prompt",
		"user prompt",
		categorizationSchema(),
		&out,
	)
	require.NoError(t, err)
	require.Len(t, out.Assignments, 1)
}

func TestOllamaClientChatJSON_ReportsThinkingWithoutContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"message": {
				"content": "",
				"thinking": "step by step"
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := &OllamaClient{
		host:       server.URL,
		model:      "test-model",
		httpClient: server.Client(),
		traceDir:   t.TempDir(),
	}

	var out EditorialCategorization
	err := client.ChatJSON(
		context.Background(),
		"categorize batch 0 10",
		"system prompt",
		"user prompt",
		categorizationSchema(),
		&out,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thinking text without final content")
}

func TestOllamaClientChatJSON_RetriesAfterPlainTextResponse(t *testing.T) {
	requestCount := 0
	var secondAttempt ollamaChatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			_, err := w.Write([]byte(`{
				"message": {
					"content": "I can't classify this batch."
				}
			}`))
			require.NoError(t, err)
			return
		}

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &secondAttempt))
		_, err = w.Write([]byte(`{
			"message": {
				"content": "{\"assignments\":[{\"rkey\":\"abc123\",\"section_id\":\"tech\"}]}"
			}
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := &OllamaClient{
		host:       server.URL,
		model:      "test-model",
		httpClient: server.Client(),
		traceDir:   t.TempDir(),
	}

	var out EditorialCategorization
	err := client.ChatJSON(
		context.Background(),
		"categorize batch 0 10",
		"system prompt",
		"user prompt",
		categorizationSchema(),
		&out,
	)
	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
	require.NotNil(t, secondAttempt.Think)
	assert.False(t, *secondAttempt.Think)
	assert.Contains(t, secondAttempt.Messages[0].Content, "Previous attempt returned prose or invalid JSON")
	assert.Len(t, out.Assignments, 1)
}

func TestSanitizeTraceLabel(t *testing.T) {
	assert.Equal(t, "headlines-tech-ai", sanitizeTraceLabel("Headlines: tech/ai"))
	assert.Equal(t, "ollama-call", sanitizeTraceLabel("   "))
}
