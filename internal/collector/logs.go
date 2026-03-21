package collector

import (
	"bufio"
	"context"
	"io"
	"os"
	"sync"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type LogCollector struct {
	mu      sync.Mutex
	offsets map[string]int64
}

func NewLogCollector() *LogCollector {
	return &LogCollector{
		offsets: make(map[string]int64),
	}
}

func (c *LogCollector) Collect(ctx context.Context, cfg *config.Runtime) ([]payload.LogEntry, []string) {
	if len(cfg.Logs) == 0 {
		return nil, nil
	}

	var allEntries []payload.LogEntry
	var warnings []string

	for _, logCfg := range cfg.Logs {
		if !logCfg.Enabled {
			continue
		}

		entries, err := c.collectFile(logCfg.Path, logCfg.Name)
		if err != nil {
			warnings = append(warnings, "failed to collect log file "+logCfg.Path+": "+err.Error())
			continue
		}
		allEntries = append(allEntries, entries...)
	}

	// Limit total entries per snapshot to avoid huge payloads
	if len(allEntries) > 500 {
		allEntries = allEntries[len(allEntries)-500:]
	}

	return allEntries, warnings
}

func (c *LogCollector) collectFile(path string, name string) ([]payload.LogEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	lastOffset := c.offsets[path]
	if lastOffset == 0 {
		// First time seeing this file, start from the end
		c.offsets[path] = info.Size()
		return nil, nil
	}

	if info.Size() < lastOffset {
		// File truncated, start from the beginning
		lastOffset = 0
	}

	if _, err := file.Seek(lastOffset, io.SeekStart); err != nil {
		return nil, err
	}

	var entries []payload.LogEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		entries = append(entries, payload.LogEntry{
			CapturedAt: time.Now().UTC(),
			Source:     name,
			Path:       path,
			Message:    line,
		})
		// Hard limit per file per collection
		if len(entries) >= 200 {
			break
		}
	}

	newOffset, _ := file.Seek(0, io.SeekCurrent)
	c.offsets[path] = newOffset

	return entries, scanner.Err()
}
