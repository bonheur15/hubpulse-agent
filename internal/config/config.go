package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultCollectorURL = "https://collector.HubPulse.space/ingest"
	DefaultConfigPath   = "/etc/hubpulse-agent/config.json"
	defaultSpoolDir     = "data/spool"
)

type File struct {
	AgentID      string            `json:"agent_id"`
	Token        string            `json:"token"`
	CollectorURL string            `json:"collector_url"`
	Hostname     string            `json:"hostname"`
	Labels       map[string]string `json:"labels"`
	Collection   CollectionFile    `json:"collection"`
	Processes    ProcessFile       `json:"processes"`
	Services     []ServiceFile     `json:"services"`
	Buffer       BufferFile        `json:"buffer"`
	Transport    TransportFile     `json:"transport"`
	Logging      LoggingFile       `json:"logging"`
}

type CollectionFile struct {
	MetricsInterval      string `json:"metrics_interval"`
	ProcessInterval      string `json:"process_interval"`
	ServiceInterval      string `json:"service_interval"`
	FlushInterval        string `json:"flush_interval"`
	BufferDrainInterval  string `json:"buffer_drain_interval"`
	ConfigReloadInterval string `json:"config_reload_interval"`
	CPUSampleInterval    string `json:"cpu_sample_interval"`
	IncludeDiskIO        *bool  `json:"include_disk_io"`
	IncludeConnections   *bool  `json:"include_connections"`
}

type ProcessFile struct {
	Enabled         *bool    `json:"enabled"`
	MaxProcesses    int      `json:"max_processes"`
	IncludeCommand  *bool    `json:"include_command"`
	IncludeExecPath *bool    `json:"include_exec_path"`
	IncludePatterns []string `json:"include_patterns"`
	ExcludePatterns []string `json:"exclude_patterns"`
}

type ServiceFile struct {
	Name               string            `json:"name"`
	Type               string            `json:"type"`
	Target             string            `json:"target"`
	Interval           string            `json:"interval"`
	Timeout            string            `json:"timeout"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	ExpectedStatuses   []int             `json:"expected_statuses"`
	BodyContains       string            `json:"body_contains"`
	BodyNotContains    string            `json:"body_not_contains"`
	InsecureSkipVerify bool              `json:"insecure_skip_verify"`
	Enabled            *bool             `json:"enabled"`
}

type BufferFile struct {
	Enabled       *bool  `json:"enabled"`
	Directory     string `json:"directory"`
	MaxBatches    int    `json:"max_batches"`
	MaxBatchBytes int64  `json:"max_batch_bytes"`
	MaxBatchAge   string `json:"max_batch_age"`
}

type TransportFile struct {
	BatchSize      int    `json:"batch_size"`
	RequestTimeout string `json:"request_timeout"`
	MaxRetries     int    `json:"max_retries"`
	InitialBackoff string `json:"initial_backoff"`
	MaxBackoff     string `json:"max_backoff"`
	Compression    *bool  `json:"compression"`
	UserAgent      string `json:"user_agent"`
}

type LoggingFile struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

type Runtime struct {
	AgentID        string
	Token          string
	CollectorURL   string
	Hostname       string
	Labels         map[string]string
	Collection     CollectionRuntime
	Processes      ProcessRuntime
	Services       []ServiceRuntime
	Buffer         BufferRuntime
	Transport      TransportRuntime
	Logging        LoggingRuntime
	ConfigPath     string
	ConfigRevision string
	Warnings       []string
}

type CollectionRuntime struct {
	MetricsInterval      time.Duration
	ProcessInterval      time.Duration
	ServiceInterval      time.Duration
	FlushInterval        time.Duration
	BufferDrainInterval  time.Duration
	ConfigReloadInterval time.Duration
	CPUSampleInterval    time.Duration
	IncludeDiskIO        bool
	IncludeConnections   bool
}

type ProcessRuntime struct {
	Enabled         bool
	MaxProcesses    int
	IncludeCommand  bool
	IncludeExecPath bool
	IncludePatterns []string
	ExcludePatterns []string
}

type ServiceRuntime struct {
	Name               string
	Type               string
	Target             string
	Interval           time.Duration
	Timeout            time.Duration
	Method             string
	Headers            map[string]string
	ExpectedStatuses   []int
	BodyContains       string
	BodyNotContains    string
	InsecureSkipVerify bool
}

type BufferRuntime struct {
	Enabled       bool
	Directory     string
	MaxBatches    int
	MaxBatchBytes int64
	MaxBatchAge   time.Duration
}

type TransportRuntime struct {
	BatchSize      int
	RequestTimeout time.Duration
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Compression    bool
	UserAgent      string
}

type LoggingRuntime struct {
	Level  string
	Format string
}

func DefaultFile() File {
	return File{
		CollectorURL: DefaultCollectorURL,
		Labels:       map[string]string{},
		Collection: CollectionFile{
			MetricsInterval:      "15s",
			ProcessInterval:      "30s",
			ServiceInterval:      "15s",
			FlushInterval:        "15s",
			BufferDrainInterval:  "20s",
			ConfigReloadInterval: "10s",
			CPUSampleInterval:    "500ms",
			IncludeDiskIO:        boolPtr(true),
			IncludeConnections:   boolPtr(true),
		},
		Processes: ProcessFile{
			Enabled:         boolPtr(true),
			MaxProcesses:    25,
			IncludeCommand:  boolPtr(true),
			IncludeExecPath: boolPtr(true),
			IncludePatterns: []string{},
			ExcludePatterns: []string{},
		},
		Services: []ServiceFile{},
		Buffer: BufferFile{
			Enabled:       boolPtr(true),
			Directory:     defaultSpoolDir,
			MaxBatches:    512,
			MaxBatchBytes: 2 << 20,
			MaxBatchAge:   "168h",
		},
		Transport: TransportFile{
			BatchSize:      10,
			RequestTimeout: "10s",
			MaxRetries:     3,
			InitialBackoff: "2s",
			MaxBackoff:     "30s",
			Compression:    boolPtr(true),
			UserAgent:      "hubpulse-agent",
		},
		Logging: LoggingFile{
			Level:  "info",
			Format: "text",
		},
	}
}

func DefaultRuntime() *Runtime {
	rt, _ := runtimeFromFile(DefaultFile(), "")
	return rt
}

func DefaultConfigJSON() ([]byte, error) {
	cfg := DefaultFile()
	return json.MarshalIndent(cfg, "", "  ")
}

func LoadFile(path string) (*Runtime, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Parse(raw, path)
}

func Parse(raw []byte, source string) (*Runtime, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		rt := DefaultRuntime()
		rt.ConfigPath = source
		rt.Warnings = append(rt.Warnings, "configuration file is empty; using defaults")
		rt.ConfigRevision = hashBytes(raw)
		return rt, nil
	}

	var fileCfg File
	if err := json.Unmarshal(raw, &fileCfg); err != nil {
		return nil, err
	}

	rt, err := runtimeFromFile(fileCfg, source)
	if err != nil {
		return nil, err
	}
	rt.ConfigRevision = hashBytes(raw)
	return rt, nil
}

func DecodeBase64Config(encoded string) ([]byte, error) {
	cleaned := strings.TrimSpace(encoded)
	if cleaned == "" {
		return nil, errors.New("base64 config payload is empty")
	}

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(cleaned)
		if err == nil {
			return decoded, nil
		}
	}

	return nil, errors.New("failed to decode base64 config payload")
}

func WriteFileAtomically(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".hubpulse-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmpName, path)
}

func runtimeFromFile(fileCfg File, source string) (*Runtime, error) {
	defaults := DefaultFile()
	warnings := make([]string, 0, 8)

	hostname := sanitizeString(fileCfg.Hostname)
	if hostname == "" {
		systemHostname, err := os.Hostname()
		if err != nil || sanitizeString(systemHostname) == "" {
			hostname = "unknown-host"
			warnings = append(warnings, "hostname could not be detected; using unknown-host")
		} else {
			hostname = sanitizeString(systemHostname)
		}
	}

	agentID := sanitizeString(fileCfg.AgentID)
	if agentID == "" {
		agentID = hostname
		warnings = append(warnings, "agent_id is empty; using hostname as the server identity")
	}

	collectorURL := sanitizeString(fileCfg.CollectorURL)
	if collectorURL == "" {
		collectorURL = defaults.CollectorURL
	}
	if _, err := url.ParseRequestURI(collectorURL); err != nil {
		warnings = append(warnings, fmt.Sprintf("collector_url %q is invalid; using default %s", collectorURL, defaults.CollectorURL))
		collectorURL = defaults.CollectorURL
	}

	labels := sanitizeLabels(fileCfg.Labels)
	collection := CollectionRuntime{
		MetricsInterval:      parseDurationWithDefault(fileCfg.Collection.MetricsInterval, defaults.Collection.MetricsInterval, "collection.metrics_interval", &warnings),
		ProcessInterval:      parseDurationWithDefault(fileCfg.Collection.ProcessInterval, defaults.Collection.ProcessInterval, "collection.process_interval", &warnings),
		ServiceInterval:      parseDurationWithDefault(fileCfg.Collection.ServiceInterval, defaults.Collection.ServiceInterval, "collection.service_interval", &warnings),
		FlushInterval:        parseDurationWithDefault(fileCfg.Collection.FlushInterval, defaults.Collection.FlushInterval, "collection.flush_interval", &warnings),
		BufferDrainInterval:  parseDurationWithDefault(fileCfg.Collection.BufferDrainInterval, defaults.Collection.BufferDrainInterval, "collection.buffer_drain_interval", &warnings),
		ConfigReloadInterval: parseDurationWithDefault(fileCfg.Collection.ConfigReloadInterval, defaults.Collection.ConfigReloadInterval, "collection.config_reload_interval", &warnings),
		CPUSampleInterval:    parseDurationWithDefault(fileCfg.Collection.CPUSampleInterval, defaults.Collection.CPUSampleInterval, "collection.cpu_sample_interval", &warnings),
		IncludeDiskIO:        valueOrDefault(fileCfg.Collection.IncludeDiskIO, defaults.Collection.IncludeDiskIO),
		IncludeConnections:   valueOrDefault(fileCfg.Collection.IncludeConnections, defaults.Collection.IncludeConnections),
	}

	processes := ProcessRuntime{
		Enabled:         valueOrDefault(fileCfg.Processes.Enabled, defaults.Processes.Enabled),
		MaxProcesses:    normalizeInt(fileCfg.Processes.MaxProcesses, defaults.Processes.MaxProcesses, "processes.max_processes", &warnings),
		IncludeCommand:  valueOrDefault(fileCfg.Processes.IncludeCommand, defaults.Processes.IncludeCommand),
		IncludeExecPath: valueOrDefault(fileCfg.Processes.IncludeExecPath, defaults.Processes.IncludeExecPath),
		IncludePatterns: sanitizeList(fileCfg.Processes.IncludePatterns),
		ExcludePatterns: sanitizeList(fileCfg.Processes.ExcludePatterns),
	}

	bufferDir := sanitizeString(fileCfg.Buffer.Directory)
	if bufferDir == "" {
		bufferDir = defaults.Buffer.Directory
	}
	bufferDir = resolvePath(source, bufferDir)
	bufferRuntime := BufferRuntime{
		Enabled:       valueOrDefault(fileCfg.Buffer.Enabled, defaults.Buffer.Enabled),
		Directory:     bufferDir,
		MaxBatches:    normalizeInt(fileCfg.Buffer.MaxBatches, defaults.Buffer.MaxBatches, "buffer.max_batches", &warnings),
		MaxBatchBytes: normalizeInt64(fileCfg.Buffer.MaxBatchBytes, defaults.Buffer.MaxBatchBytes, "buffer.max_batch_bytes", &warnings),
		MaxBatchAge:   parseDurationWithDefault(fileCfg.Buffer.MaxBatchAge, defaults.Buffer.MaxBatchAge, "buffer.max_batch_age", &warnings),
	}

	transport := TransportRuntime{
		BatchSize:      normalizeInt(fileCfg.Transport.BatchSize, defaults.Transport.BatchSize, "transport.batch_size", &warnings),
		RequestTimeout: parseDurationWithDefault(fileCfg.Transport.RequestTimeout, defaults.Transport.RequestTimeout, "transport.request_timeout", &warnings),
		MaxRetries:     normalizeInt(fileCfg.Transport.MaxRetries, defaults.Transport.MaxRetries, "transport.max_retries", &warnings),
		InitialBackoff: parseDurationWithDefault(fileCfg.Transport.InitialBackoff, defaults.Transport.InitialBackoff, "transport.initial_backoff", &warnings),
		MaxBackoff:     parseDurationWithDefault(fileCfg.Transport.MaxBackoff, defaults.Transport.MaxBackoff, "transport.max_backoff", &warnings),
		Compression:    valueOrDefault(fileCfg.Transport.Compression, defaults.Transport.Compression),
		UserAgent:      sanitizeString(fileCfg.Transport.UserAgent),
	}
	if transport.UserAgent == "" {
		transport.UserAgent = defaults.Transport.UserAgent
	}

	logging := LoggingRuntime{
		Level:  normalizeLevel(fileCfg.Logging.Level, defaults.Logging.Level, &warnings),
		Format: normalizeFormat(fileCfg.Logging.Format, defaults.Logging.Format, &warnings),
	}

	services, serviceWarnings, err := sanitizeServices(fileCfg.Services, collection.ServiceInterval)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, serviceWarnings...)

	token := strings.TrimSpace(fileCfg.Token)
	if token == "" {
		warnings = append(warnings, "token is empty; delivery to the collector will be disabled until a token is configured")
	}

	return &Runtime{
		AgentID:      agentID,
		Token:        token,
		CollectorURL: collectorURL,
		Hostname:     hostname,
		Labels:       labels,
		Collection:   collection,
		Processes:    processes,
		Services:     services,
		Buffer:       bufferRuntime,
		Transport:    transport,
		Logging:      logging,
		ConfigPath:   source,
		Warnings:     dedupeWarnings(warnings),
	}, nil
}

func sanitizeServices(services []ServiceFile, defaultInterval time.Duration) ([]ServiceRuntime, []string, error) {
	if len(services) == 0 {
		return nil, nil, nil
	}

	defaultTimeout := 3 * time.Second
	warnings := make([]string, 0, len(services))
	results := make([]ServiceRuntime, 0, len(services))
	seenNames := map[string]int{}

	for idx, service := range services {
		if service.Enabled != nil && !*service.Enabled {
			continue
		}

		serviceType := strings.ToLower(sanitizeString(service.Type))
		if serviceType != "http" && serviceType != "tcp" {
			warnings = append(warnings, fmt.Sprintf("services[%d] has unsupported type %q and was skipped", idx, service.Type))
			continue
		}

		target := sanitizeString(service.Target)
		if target == "" {
			warnings = append(warnings, fmt.Sprintf("services[%d] is missing target and was skipped", idx))
			continue
		}

		if serviceType == "http" {
			parsedURL, err := url.Parse(target)
			if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
				warnings = append(warnings, fmt.Sprintf("services[%d] target %q is not a valid HTTP URL and was skipped", idx, target))
				continue
			}
		}
		if serviceType == "tcp" {
			if _, err := net.ResolveTCPAddr("tcp", target); err != nil {
				warnings = append(warnings, fmt.Sprintf("services[%d] target %q is not a valid TCP address and was skipped", idx, target))
				continue
			}
		}

		name := sanitizeString(service.Name)
		if name == "" {
			name = deriveServiceName(serviceType, target, idx)
		}
		if count := seenNames[name]; count > 0 {
			newName := fmt.Sprintf("%s-%d", name, count+1)
			warnings = append(warnings, fmt.Sprintf("service name %q is duplicated; renamed to %q", name, newName))
			name = newName
		}
		seenNames[name]++

		interval := parseDurationWithFallback(service.Interval, defaultInterval, fmt.Sprintf("services[%s].interval", name), &warnings)
		timeout := parseDurationWithFallback(service.Timeout, defaultTimeout, fmt.Sprintf("services[%s].timeout", name), &warnings)
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		if interval < timeout {
			timeout = interval
		}

		headers := map[string]string{}
		for key, value := range service.Headers {
			cleanKey := httpHeaderName(key)
			cleanValue := strings.TrimSpace(value)
			if cleanKey == "" || cleanValue == "" {
				continue
			}
			headers[cleanKey] = cleanValue
		}

		expectedStatuses := sanitizeStatuses(service.ExpectedStatuses)
		results = append(results, ServiceRuntime{
			Name:               name,
			Type:               serviceType,
			Target:             target,
			Interval:           interval,
			Timeout:            timeout,
			Method:             strings.ToUpper(strings.TrimSpace(defaultIfEmpty(service.Method, "GET"))),
			Headers:            headers,
			ExpectedStatuses:   expectedStatuses,
			BodyContains:       strings.TrimSpace(service.BodyContains),
			BodyNotContains:    strings.TrimSpace(service.BodyNotContains),
			InsecureSkipVerify: service.InsecureSkipVerify,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, warnings, nil
}

func ResolveConfigPath(path string) string {
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		return trimmed
	}
	if envPath := strings.TrimSpace(os.Getenv("HUBPULSE_CONFIG_PATH")); envPath != "" {
		return envPath
	}
	if _, err := os.Stat(DefaultConfigPath); err == nil {
		return DefaultConfigPath
	}
	return "hubpulse-agent.json"
}

func boolPtr(v bool) *bool {
	return &v
}

func valueOrDefault(value *bool, defaultValue *bool) bool {
	if value != nil {
		return *value
	}
	if defaultValue != nil {
		return *defaultValue
	}
	return false
}

func parseDurationWithDefault(raw string, defaultRaw string, field string, warnings *[]string) time.Duration {
	defaultDuration, err := time.ParseDuration(defaultRaw)
	if err != nil {
		defaultDuration = time.Second
	}
	return parseDurationWithFallback(raw, defaultDuration, field, warnings)
}

func parseDurationWithFallback(raw string, fallback time.Duration, field string, warnings *[]string) time.Duration {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		*warnings = append(*warnings, fmt.Sprintf("%s has invalid duration %q; using %s", field, raw, fallback))
		return fallback
	}
	return value
}

func normalizeInt(value int, fallback int, field string, warnings *[]string) int {
	if value > 0 {
		return value
	}
	if value < 0 {
		*warnings = append(*warnings, fmt.Sprintf("%s must be positive; using %d", field, fallback))
	}
	return fallback
}

func normalizeInt64(value int64, fallback int64, field string, warnings *[]string) int64 {
	if value > 0 {
		return value
	}
	if value < 0 {
		*warnings = append(*warnings, fmt.Sprintf("%s must be positive; using %d", field, fallback))
	}
	return fallback
}

func normalizeLevel(value string, fallback string, warnings *[]string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "debug", "info", "warn", "error":
		if strings.TrimSpace(value) == "" {
			return fallback
		}
		return strings.ToLower(strings.TrimSpace(value))
	default:
		*warnings = append(*warnings, fmt.Sprintf("logging.level %q is invalid; using %s", value, fallback))
		return fallback
	}
}

func normalizeFormat(value string, fallback string, warnings *[]string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "text", "json":
		if strings.TrimSpace(value) == "" {
			return fallback
		}
		return strings.ToLower(strings.TrimSpace(value))
	default:
		*warnings = append(*warnings, fmt.Sprintf("logging.format %q is invalid; using %s", value, fallback))
		return fallback
	}
}

func sanitizeString(value string) string {
	return strings.TrimSpace(value)
}

func sanitizeList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		cleaned := strings.ToLower(strings.TrimSpace(value))
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	sort.Strings(result)
	return result
}

func sanitizeLabels(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	labels := make(map[string]string, len(input))
	for key, value := range input {
		cleanKey := strings.TrimSpace(key)
		cleanValue := strings.TrimSpace(value)
		if cleanKey == "" || cleanValue == "" {
			continue
		}
		labels[cleanKey] = cleanValue
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

func sanitizeStatuses(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value < 100 || value > 599 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolvePath(configPath string, target string) string {
	if filepath.IsAbs(target) || target == "" {
		return target
	}
	baseDir := "."
	if trimmed := strings.TrimSpace(configPath); trimmed != "" {
		baseDir = filepath.Dir(trimmed)
	}
	return filepath.Clean(filepath.Join(baseDir, target))
}

func deriveServiceName(serviceType string, target string, idx int) string {
	switch serviceType {
	case "http":
		parsedURL, err := url.Parse(target)
		if err == nil && parsedURL.Host != "" {
			name := parsedURL.Hostname()
			if parsedURL.Path != "" && parsedURL.Path != "/" {
				pathPart := strings.Trim(strings.ReplaceAll(parsedURL.Path, "/", "-"), "-")
				if pathPart != "" {
					name = fmt.Sprintf("%s-%s", name, pathPart)
				}
			}
			if name != "" {
				return name
			}
		}
	case "tcp":
		host, port, err := net.SplitHostPort(target)
		if err == nil {
			host = strings.Trim(strings.ReplaceAll(host, ".", "-"), "[]")
			host = strings.ReplaceAll(host, ":", "-")
			if host == "" {
				host = "localhost"
			}
			return fmt.Sprintf("%s-%s", host, port)
		}
	}
	return fmt.Sprintf("service-%d", idx+1)
}

func httpHeaderName(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, part := range parts {
		parts[i] = strings.Title(strings.ToLower(part))
	}
	return strings.Join(parts, "-")
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func dedupeWarnings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
