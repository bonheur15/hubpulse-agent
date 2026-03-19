package config

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSanitizesInvalidValues(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
	  "collector_url": "://bad",
	  "collection": {
	    "metrics_interval": "nope",
	    "flush_interval": "0s"
	  },
	  "processes": {
	    "max_processes": -1
	  },
	  "buffer": {
	    "directory": "spool",
	    "max_batches": -2
	  },
	  "services": [
	    {
	      "type": "http",
	      "target": "http://localhost:8080/health"
	    },
	    {
	      "name": "db",
	      "type": "tcp",
	      "target": "127.0.0.1:5432"
	    },
	    {
	      "name": "db",
	      "type": "tcp",
	      "target": "127.0.0.1:5433"
	    },
	    {
	      "type": "bogus",
	      "target": "x"
	    }
	  ]
	}`)

	cfg, err := Parse(raw, "/tmp/hubpulse/config.json")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if cfg.CollectorURL != DefaultCollectorURL {
		t.Fatalf("expected default collector URL, got %q", cfg.CollectorURL)
	}
	if cfg.Collection.MetricsInterval <= 0 {
		t.Fatalf("expected positive metrics interval, got %s", cfg.Collection.MetricsInterval)
	}
	if cfg.Processes.MaxProcesses != DefaultFile().Processes.MaxProcesses {
		t.Fatalf("expected default max processes, got %d", cfg.Processes.MaxProcesses)
	}

	expectedBufferDir := filepath.Clean("/tmp/hubpulse/spool")
	if cfg.Buffer.Directory != expectedBufferDir {
		t.Fatalf("expected resolved buffer dir %q, got %q", expectedBufferDir, cfg.Buffer.Directory)
	}

	if len(cfg.Services) != 3 {
		t.Fatalf("expected 3 valid services, got %d", len(cfg.Services))
	}
	names := []string{cfg.Services[0].Name, cfg.Services[1].Name, cfg.Services[2].Name}
	if names[0] != "db" || names[1] != "db-2" || names[2] != "localhost-health" {
		t.Fatalf("expected duplicate names to be normalized, got %+v", cfg.Services)
	}

	if !containsWarning(cfg.Warnings, "collector_url") {
		t.Fatalf("expected collector_url warning, got %v", cfg.Warnings)
	}
	if !containsWarning(cfg.Warnings, "renamed to") {
		t.Fatalf("expected duplicate service warning, got %v", cfg.Warnings)
	}
}

func TestDecodeBase64Config(t *testing.T) {
	t.Parallel()

	source := `{"token":"abc"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(source))

	decoded, err := DecodeBase64Config(encoded)
	if err != nil {
		t.Fatalf("DecodeBase64Config returned error: %v", err)
	}
	if string(decoded) != source {
		t.Fatalf("decoded payload mismatch: got %q", string(decoded))
	}
}

func containsWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}
