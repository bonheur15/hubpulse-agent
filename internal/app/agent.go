package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"hubpulse-agent/internal/buffer"
	"hubpulse-agent/internal/collector"
	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
	"hubpulse-agent/internal/sender"
	"hubpulse-agent/internal/version"
)

type Agent struct {
	logger     *slog.Logger
	levelVar   *slog.LevelVar
	configMu   sync.RWMutex
	config     *config.Runtime
	configStat fileStamp
	cache      *snapshotCache
	queue      *sampleQueue
	system     *collector.SystemCollector
	process    *collector.ProcessCollector
	services   *collector.ServiceCollector
	logs       *collector.LogCollector
	sender     *sender.Client
}

type fileStamp struct {
	modTime time.Time
	size    int64
}

type sampleQueue struct {
	mu      sync.Mutex
	samples []payload.Snapshot
}

type snapshotCache struct {
	mu              sync.RWMutex
	system          payload.SystemMetrics
	hasSystem       bool
	processes       []payload.ProcessStat
	services        []payload.ServiceCheckResult
	logs            []payload.LogEntry
	systemWarnings  []string
	processWarnings []string
	serviceWarnings []string
	logWarnings     []string
	dirty           bool
}

func NewAgent(logger *slog.Logger, levelVar *slog.LevelVar, cfg *config.Runtime, stamp fileStamp) *Agent {
	return &Agent{
		logger:     logger,
		levelVar:   levelVar,
		config:     cfg,
		configStat: stamp,
		cache:      &snapshotCache{},
		queue:      &sampleQueue{},
		system:     collector.NewSystemCollector(),
		process:    collector.NewProcessCollector(),
		services:   collector.NewServiceCollector(),
		logs:       collector.NewLogCollector(),
		sender:     sender.New(),
	}
}

func Run(ctx context.Context, configPath string, once bool, printSnapshot bool) error {
	cfg, stamp, err := loadRuntimeConfigWithFallback(configPath)
	if err != nil {
		return err
	}

	levelVar := &slog.LevelVar{}
	levelVar.Set(slogLevel(cfg.Logging.Level))
	logger := newLogger(cfg, levelVar)
	agent := NewAgent(logger, levelVar, cfg, stamp)
	agent.logConfigWarnings(cfg)

	if once {
		snapshot, err := agent.CollectSnapshot(ctx)
		if err != nil {
			return err
		}
		if printSnapshot {
			if err := printJSON(snapshot); err != nil {
				return err
			}
			return nil
		}
		if err := agent.sendBatch(ctx, cfg, []payload.Snapshot{snapshot}); err != nil {
			return err
		}
		return nil
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wait sync.WaitGroup
	runLoop := func(name string, fn func(context.Context)) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer agent.recoverLoop(name)
			fn(runCtx)
		}()
	}

	runLoop("system", agent.systemLoop)
	runLoop("process", agent.processLoop)
	runLoop("services", agent.serviceLoop)
	runLoop("logs", agent.logsLoop)
	runLoop("flush", agent.flushLoop)
	runLoop("drain", agent.drainLoop)
	runLoop("reload", agent.reloadLoop)

	<-runCtx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	agent.flushOnce(shutdownCtx)
	agent.drainOnce(shutdownCtx)

	wait.Wait()
	return nil
}

func (a *Agent) CollectSnapshot(ctx context.Context) (payload.Snapshot, error) {
	cfg := a.currentConfig()
	systemMetrics, systemWarnings := a.system.Collect(ctx, cfg)
	processes, processWarnings := a.process.Collect(ctx, cfg)
	services, _ := a.services.CollectDue(ctx, cfg)
	logs, logWarnings := a.logs.Collect(ctx, cfg)

	return payload.Snapshot{
		CapturedAt: time.Now().UTC(),
		System:     &systemMetrics,
		Processes:  processes,
		Services:   services,
		Logs:       logs,
		Warnings:   mergeWarnings(cfg.Warnings, systemWarnings, processWarnings, logWarnings),
	}, nil
}

func ValidateConfig(path string) error {
	cfg, _, err := loadRuntimeConfigStrict(path)
	if err != nil {
		return err
	}
	if err := printJSON(cfg); err != nil {
		return err
	}
	return nil
}

func InitConfig(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config file %s already exists; use --force to overwrite it", path)
		}
	}
	data, err := config.DefaultConfigJSON()
	if err != nil {
		return err
	}
	return config.WriteFileAtomically(path, append(data, '\n'), 0o600)
}

func UpdateConfig(path string, encoded string) error {
	decoded, err := config.DecodeBase64Config(encoded)
	if err != nil {
		return err
	}
	if _, err := config.Parse(decoded, path); err != nil {
		return fmt.Errorf("decoded config is invalid: %w", err)
	}
	return config.WriteFileAtomically(path, append(decoded, '\n'), 0o600)
}

func PrintDefaultConfig() error {
	data, err := config.DefaultConfigJSON()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

func (a *Agent) systemLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, cfg.Collection.MetricsInterval); err != nil {
			return
		}
		a.safeStep("system", func() {
			metrics, warnings := a.system.Collect(ctx, cfg)
			a.cache.setSystem(metrics, warnings)
		})
	}
}

func (a *Agent) processLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, cfg.Collection.ProcessInterval); err != nil {
			return
		}
		a.safeStep("process", func() {
			processes, warnings := a.process.Collect(ctx, cfg)
			a.cache.setProcesses(processes, warnings)
		})
	}
}

func (a *Agent) serviceLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, minDuration(cfg.Collection.ServiceInterval, time.Second)); err != nil {
			return
		}
		a.safeStep("services", func() {
			results, changed := a.services.CollectDue(ctx, cfg)
			if changed {
				a.cache.setServices(results, nil)
			}
		})
	}
}

func (a *Agent) logsLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		// Logs are collected fairly frequently if configured
		if err := sleepWithContext(ctx, 10*time.Second); err != nil {
			return
		}
		a.safeStep("logs", func() {
			logs, warnings := a.logs.Collect(ctx, cfg)
			if len(logs) > 0 || len(warnings) > 0 {
				a.cache.setLogs(logs, warnings)
			}
		})
	}
}

func (a *Agent) flushLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, cfg.Collection.FlushInterval); err != nil {
			return
		}
		a.flushOnce(ctx)
	}
}

func (a *Agent) drainLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, cfg.Collection.BufferDrainInterval); err != nil {
			return
		}
		a.drainOnce(ctx)
	}
}

func (a *Agent) reloadLoop(ctx context.Context) {
	for {
		cfg := a.currentConfig()
		if err := sleepWithContext(ctx, cfg.Collection.ConfigReloadInterval); err != nil {
			return
		}
		a.safeStep("reload", func() {
			if err := a.reloadConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
				a.logger.Warn("config reload failed", "error", err)
			}
		})
	}
}

func (a *Agent) flushOnce(ctx context.Context) {
	cfg := a.currentConfig()
	if snapshot, ok := a.cache.popSnapshot(cfg.Warnings); ok {
		a.queue.push(snapshot)
	}
	if err := a.deliverQueued(ctx, cfg); err != nil {
		a.logger.Warn("flush failed", "error", err)
	}
}

func (a *Agent) drainOnce(ctx context.Context) {
	cfg := a.currentConfig()
	spool := buffer.New(cfg.Buffer)
	if !spool.Enabled() {
		return
	}

	entries, err := spool.List()
	if err != nil {
		a.logger.Warn("failed to list buffered batches", "error", err)
		return
	}

	for _, entry := range entries {
		batch, err := spool.Load(entry.Path)
		if err != nil {
			a.logger.Warn("failed to decode buffered batch; deleting corrupt file", "path", entry.Path, "error", err)
			_ = spool.Delete(entry.Path)
			continue
		}

		err = a.sender.Send(ctx, cfg, batch)
		if err == nil {
			_ = spool.Delete(entry.Path)
			continue
		}

		if sender.IsTemporary(err) {
			a.logger.Warn("buffer drain paused due to collector error", "error", err)
			return
		}

		a.logger.Warn("dropping permanently invalid buffered batch", "path", entry.Path, "error", err)
		_ = spool.Delete(entry.Path)
	}
}

func (a *Agent) deliverQueued(ctx context.Context, cfg *config.Runtime) error {
	for {
		batchSamples := a.queue.pop(cfg.Transport.BatchSize)
		if len(batchSamples) == 0 {
			return nil
		}

		err := a.sendBatch(ctx, cfg, batchSamples)
		if err == nil {
			continue
		}

		if !sender.IsTemporary(err) {
			a.logger.Warn("dropping permanently invalid batch", "error", err, "samples", len(batchSamples))
			continue
		}

		spool := buffer.New(cfg.Buffer)
		if !spool.Enabled() {
			a.queue.requeueFront(batchSamples)
			return err
		}

		if storeErr := spool.Store(a.buildEnvelope(cfg, batchSamples)); storeErr != nil {
			a.queue.requeueFront(batchSamples)
			return fmt.Errorf("send failed (%w) and buffering also failed (%v)", err, storeErr)
		}
		a.logger.Warn("collector unreachable; batch buffered locally", "samples", len(batchSamples), "error", err)
	}
}

func (a *Agent) sendBatch(ctx context.Context, cfg *config.Runtime, samples []payload.Snapshot) error {
	if len(samples) == 0 {
		return nil
	}
	return a.sender.Send(ctx, cfg, a.buildEnvelope(cfg, samples))
}

func (a *Agent) buildEnvelope(cfg *config.Runtime, samples []payload.Snapshot) payload.Envelope {
	return payload.Envelope{
		SchemaVersion: payload.SchemaVersion,
		Agent: payload.AgentMetadata{
			ID:        cfg.AgentID,
			Hostname:  cfg.Hostname,
			Labels:    cfg.Labels,
			Version:   version.AgentVersion,
			ConfigRev: cfg.ConfigRevision,
		},
		Samples: samples,
	}
}

func (a *Agent) currentConfig() *config.Runtime {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.config
}

func (a *Agent) reloadConfig() error {
	cfg := a.currentConfig()
	info, err := os.Stat(cfg.ConfigPath)
	if err != nil {
		return err
	}
	stamp := fileStamp{modTime: info.ModTime().UTC(), size: info.Size()}
	if stamp == a.configStat {
		return nil
	}

	updated, loadedStamp, err := loadRuntimeConfigStrict(cfg.ConfigPath)
	if err != nil {
		return err
	}

	a.configMu.Lock()
	a.config = updated
	a.configStat = loadedStamp
	a.configMu.Unlock()
	a.levelVar.Set(slogLevel(updated.Logging.Level))
	a.logConfigWarnings(updated)
	a.logger.Info("config reloaded", "path", updated.ConfigPath, "revision", updated.ConfigRevision)
	return nil
}

func (a *Agent) safeStep(name string, fn func()) {
	defer a.recoverLoop(name)
	fn()
}

func (a *Agent) recoverLoop(name string) {
	if recovered := recover(); recovered != nil {
		a.logger.Error("loop recovered from panic", "loop", name, "panic", recovered)
	}
}

func (a *Agent) logConfigWarnings(cfg *config.Runtime) {
	for _, warning := range cfg.Warnings {
		a.logger.Warn("config warning", "warning", warning)
	}
}

func loadRuntimeConfigStrict(path string) (*config.Runtime, fileStamp, error) {
	resolvedPath := config.ResolveConfigPath(path)
	cfg, err := config.LoadFile(resolvedPath)
	if err != nil {
		return nil, fileStamp{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return cfg, fileStamp{}, nil
	}
	return cfg, fileStamp{modTime: info.ModTime().UTC(), size: info.Size()}, nil
}

func loadRuntimeConfigWithFallback(path string) (*config.Runtime, fileStamp, error) {
	cfg, stamp, err := loadRuntimeConfigStrict(path)
	if err == nil {
		return cfg, stamp, nil
	}

	resolvedPath := config.ResolveConfigPath(path)
	defaultCfg := config.DefaultRuntime()
	defaultCfg.ConfigPath = resolvedPath
	if errors.Is(err, os.ErrNotExist) {
		defaultCfg.Warnings = append(defaultCfg.Warnings, "config file not found; using defaults until a config is written")
	} else {
		defaultCfg.Warnings = append(defaultCfg.Warnings, "config could not be parsed; using defaults: "+err.Error())
	}
	return defaultCfg, fileStamp{}, nil
}

func newLogger(cfg *config.Runtime, levelVar *slog.LevelVar) *slog.Logger {
	options := &slog.HandlerOptions{Level: levelVar}
	var handler slog.Handler
	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, options)
	} else {
		handler = slog.NewTextHandler(os.Stderr, options)
	}
	return slog.New(handler)
}

func slogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func minDuration(values ...time.Duration) time.Duration {
	if len(values) == 0 {
		return time.Second
	}
	current := values[0]
	for _, value := range values[1:] {
		if value < current {
			current = value
		}
	}
	return current
}

func mergeWarnings(groups ...[]string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 8)
	for _, group := range groups {
		for _, item := range group {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func (q *sampleQueue) push(snapshot payload.Snapshot) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.samples = append(q.samples, snapshot)
}

func (q *sampleQueue) pop(limit int) []payload.Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || len(q.samples) == 0 {
		return nil
	}
	if len(q.samples) < limit {
		limit = len(q.samples)
	}
	out := append([]payload.Snapshot(nil), q.samples[:limit]...)
	q.samples = q.samples[limit:]
	return out
}

func (q *sampleQueue) requeueFront(batch []payload.Snapshot) {
	if len(batch) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.samples = append(append([]payload.Snapshot(nil), batch...), q.samples...)
}

func (c *snapshotCache) setSystem(metrics payload.SystemMetrics, warnings []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.system = metrics
	c.hasSystem = true
	c.systemWarnings = append([]string(nil), warnings...)
	c.dirty = true
}

func (c *snapshotCache) setProcesses(processes []payload.ProcessStat, warnings []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processes = append([]payload.ProcessStat(nil), processes...)
	c.processWarnings = append([]string(nil), warnings...)
	c.dirty = true
}

func (c *snapshotCache) setServices(services []payload.ServiceCheckResult, warnings []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services = append([]payload.ServiceCheckResult(nil), services...)
	c.serviceWarnings = append([]string(nil), warnings...)
	c.dirty = true
}

func (c *snapshotCache) setLogs(logs []payload.LogEntry, warnings []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, logs...)
	c.logWarnings = append([]string(nil), warnings...)
	// Limit logs in cache
	if len(c.logs) > 1000 {
		c.logs = c.logs[len(c.logs)-1000:]
	}
	c.dirty = true
}

func (c *snapshotCache) popSnapshot(configWarnings []string) (payload.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return payload.Snapshot{}, false
	}
	snapshot := payload.Snapshot{
		CapturedAt: time.Now().UTC(),
		Processes:  append([]payload.ProcessStat(nil), c.processes...),
		Services:   append([]payload.ServiceCheckResult(nil), c.services...),
		Logs:       append([]payload.LogEntry(nil), c.logs...),
		Warnings:   mergeWarnings(configWarnings, c.systemWarnings, c.processWarnings, c.serviceWarnings, c.logWarnings),
	}
	if c.hasSystem {
		systemCopy := c.system
		systemCopy.Disks = append([]payload.DiskUsage(nil), c.system.Disks...)
		systemCopy.DiskIO = append([]payload.DeviceIOStats(nil), c.system.DiskIO...)
		systemCopy.Network.Interfaces = append([]payload.InterfaceIO(nil), c.system.Network.Interfaces...)
		snapshot.System = &systemCopy
	}
	c.logs = nil
	c.logWarnings = nil
	c.dirty = false
	return snapshot, true
}
