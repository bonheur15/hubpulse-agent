package buffer

import (
	"testing"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

func TestSpoolStoreLoadAndPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spool := New(config.BufferRuntime{
		Enabled:       true,
		Directory:     dir,
		MaxBatches:    2,
		MaxBatchBytes: 1 << 20,
		MaxBatchAge:   time.Hour,
	})

	for i := 0; i < 3; i++ {
		batch := payload.Envelope{
			SchemaVersion: payload.SchemaVersion,
			Agent: payload.AgentMetadata{
				ID:       "agent-1",
				Hostname: "test-host",
				Version:  "dev",
			},
			Samples: []payload.Snapshot{{CapturedAt: time.Now().UTC()}},
		}
		if err := spool.Store(batch); err != nil {
			t.Fatalf("Store returned error: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	entries, err := spool.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 retained batches, got %d", len(entries))
	}

	loaded, err := spool.Load(entries[0].Path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Agent.ID != "agent-1" {
		t.Fatalf("expected agent id agent-1, got %q", loaded.Agent.ID)
	}

	if err := spool.Delete(entries[0].Path); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
}
