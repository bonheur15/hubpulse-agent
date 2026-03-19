package buffer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type Spool struct {
	enabled       bool
	dir           string
	maxBatches    int
	maxBatchBytes int64
	maxBatchAge   time.Duration
}

type Entry struct {
	Path    string
	ModTime time.Time
}

func New(cfg config.BufferRuntime) *Spool {
	return &Spool{
		enabled:       cfg.Enabled,
		dir:           cfg.Directory,
		maxBatches:    cfg.MaxBatches,
		maxBatchBytes: cfg.MaxBatchBytes,
		maxBatchAge:   cfg.MaxBatchAge,
	}
}

func (s *Spool) Enabled() bool {
	return s != nil && s.enabled
}

func (s *Spool) Store(batch payload.Envelope) error {
	if !s.Enabled() {
		return nil
	}

	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if int64(len(raw)) > s.maxBatchBytes {
		return fmt.Errorf("batch size %d exceeds offline buffer limit %d", len(raw), s.maxBatchBytes)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if err := s.Prune(); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(s.dir, ".hubpulse-batch-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	finalName := filepath.Join(s.dir, fmt.Sprintf("%d-%d.batch.json", time.Now().UTC().UnixNano(), len(raw)))
	if err := os.Rename(tmpName, finalName); err != nil {
		return err
	}

	return s.Prune()
}

func (s *Spool) List() ([]Entry, error) {
	if !s.Enabled() {
		return nil, nil
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".batch.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, Entry{
			Path:    filepath.Join(s.dir, entry.Name()),
			ModTime: info.ModTime().UTC(),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ModTime.Equal(result[j].ModTime) {
			return result[i].Path < result[j].Path
		}
		return result[i].ModTime.Before(result[j].ModTime)
	})
	return result, nil
}

func (s *Spool) Load(path string) (payload.Envelope, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return payload.Envelope{}, err
	}
	var batch payload.Envelope
	if err := json.Unmarshal(raw, &batch); err != nil {
		return payload.Envelope{}, err
	}
	return batch, nil
}

func (s *Spool) Delete(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Spool) Prune() error {
	if !s.Enabled() {
		return nil
	}

	entries, err := s.List()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	kept := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if s.maxBatchAge > 0 && now.Sub(entry.ModTime) > s.maxBatchAge {
			_ = s.Delete(entry.Path)
			continue
		}
		kept = append(kept, entry)
	}

	if len(kept) <= s.maxBatches {
		return nil
	}

	for _, entry := range kept[:len(kept)-s.maxBatches] {
		if err := s.Delete(entry.Path); err != nil {
			return err
		}
	}
	return nil
}
