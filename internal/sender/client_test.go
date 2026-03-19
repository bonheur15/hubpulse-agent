package sender

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

func TestSendFallsBackToPlainJSONWhenGzipRejected(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var encodings []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		encodings = append(encodings, r.Header.Get("Content-Encoding"))
		mu.Unlock()
		if r.Header.Get("Content-Encoding") == "gzip" {
			http.Error(w, `{"error":"gzip not supported"}`, http.StatusBadRequest)
			return
		}

		var envelope payload.Envelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode fallback request: %v", err)
		}
		if envelope.SchemaVersion != payload.SchemaVersion {
			t.Fatalf("unexpected schema version %q", envelope.SchemaVersion)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client()}
	cfg := testRuntime(server.URL, true)

	if err := client.Send(context.Background(), cfg, testEnvelope()); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(encodings) != 2 {
		t.Fatalf("expected 2 delivery attempts, got %d", len(encodings))
	}
	if encodings[0] != "gzip" {
		t.Fatalf("expected first attempt to use gzip, got %q", encodings[0])
	}
	if encodings[1] != "" {
		t.Fatalf("expected fallback attempt to be plain JSON, got %q", encodings[1])
	}
}

func TestSendIncludesCollectorResponseBodyOnPermanentFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad payload"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client()}
	cfg := testRuntime(server.URL, false)

	err := client.Send(context.Background(), cfg, testEnvelope())
	if err == nil {
		t.Fatal("expected Send to fail")
	}

	var deliveryErr *DeliveryError
	if !strings.Contains(err.Error(), `{"error":"bad payload"}`) {
		t.Fatalf("expected error to include collector response body, got %v", err)
	}
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("expected DeliveryError, got %T", err)
	}
	if deliveryErr.Temporary {
		t.Fatalf("expected permanent failure, got temporary")
	}
	if deliveryErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", deliveryErr.StatusCode)
	}
}

func testRuntime(collectorURL string, compression bool) *config.Runtime {
	return &config.Runtime{
		AgentID:        "agent-test",
		Token:          "hp_test_token",
		CollectorURL:   collectorURL,
		Hostname:       "agent-test.local",
		ConfigRevision: "test-rev",
		Transport: config.TransportRuntime{
			RequestTimeout: 500 * time.Millisecond,
			MaxRetries:     0,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     10 * time.Millisecond,
			Compression:    compression,
			UserAgent:      "hubpulse-agent-test",
		},
	}
}

func testEnvelope() payload.Envelope {
	return payload.Envelope{
		SchemaVersion: payload.SchemaVersion,
		Agent: payload.AgentMetadata{
			ID:       "agent-test",
			Hostname: "agent-test.local",
			Version:  "test",
		},
		Samples: []payload.Snapshot{
			{
				CapturedAt: time.Unix(1_700_000_000, 0).UTC(),
				Warnings:   []string{},
			},
		},
	}
}
