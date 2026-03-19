package collector

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hubpulse-agent/internal/config"
)

func TestServiceCollectorTracksHTTPTransitions(t *testing.T) {
	t.Parallel()

	var statusCode atomic.Int64
	statusCode.Store(http.StatusOK)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(statusCode.Load()))
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	collector := NewServiceCollector()
	cfg := &config.Runtime{
		Services: []config.ServiceRuntime{
			{
				Name:             "api",
				Type:             "http",
				Target:           server.URL,
				Interval:         5 * time.Millisecond,
				Timeout:          time.Second,
				Method:           http.MethodGet,
				ExpectedStatuses: []int{http.StatusOK},
				BodyContains:     "ok",
			},
		},
	}

	results, changed := collector.CollectDue(context.Background(), cfg)
	if !changed || len(results) != 1 || !results[0].Success {
		t.Fatalf("expected passing HTTP result, got changed=%v results=%+v", changed, results)
	}

	time.Sleep(10 * time.Millisecond)
	statusCode.Store(http.StatusInternalServerError)

	results, changed = collector.CollectDue(context.Background(), cfg)
	if !changed || len(results) != 1 || results[0].Success {
		t.Fatalf("expected failing HTTP result, got changed=%v results=%+v", changed, results)
	}
	if results[0].ConsecutiveFailures != 1 {
		t.Fatalf("expected consecutive failure count to be 1, got %d", results[0].ConsecutiveFailures)
	}
	if results[0].LastTransitionAt.IsZero() {
		t.Fatalf("expected last transition timestamp to be set")
	}
}

func TestServiceCollectorChecksTCPPort(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	collector := NewServiceCollector()
	cfg := &config.Runtime{
		Services: []config.ServiceRuntime{
			{
				Name:     "db-port",
				Type:     "tcp",
				Target:   listener.Addr().String(),
				Interval: 5 * time.Millisecond,
				Timeout:  time.Second,
			},
		},
	}

	results, changed := collector.CollectDue(context.Background(), cfg)
	if !changed || len(results) != 1 || !results[0].Success {
		t.Fatalf("expected reachable TCP check, got changed=%v results=%+v", changed, results)
	}
}
