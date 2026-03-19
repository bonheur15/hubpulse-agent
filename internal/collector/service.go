package collector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type ServiceCollector struct {
	mu      sync.Mutex
	nextRun map[string]time.Time
	results map[string]payload.ServiceCheckResult
	states  map[string]serviceState
}

type serviceState struct {
	lastSuccess          bool
	consecutiveSuccesses int
	consecutiveFailures  int
	lastTransitionAt     time.Time
}

func NewServiceCollector() *ServiceCollector {
	return &ServiceCollector{
		nextRun: map[string]time.Time{},
		results: map[string]payload.ServiceCheckResult{},
		states:  map[string]serviceState{},
	}
}

func (c *ServiceCollector) CollectDue(ctx context.Context, cfg *config.Runtime) ([]payload.ServiceCheckResult, bool) {
	if len(cfg.Services) == 0 {
		c.mu.Lock()
		changed := len(c.results) > 0
		c.nextRun = map[string]time.Time{}
		c.results = map[string]payload.ServiceCheckResult{}
		c.states = map[string]serviceState{}
		c.mu.Unlock()
		return nil, changed
	}

	now := time.Now().UTC()
	due := make([]config.ServiceRuntime, 0, len(cfg.Services))
	seen := map[string]struct{}{}

	c.mu.Lock()
	for _, service := range cfg.Services {
		seen[service.Name] = struct{}{}
		nextRun, ok := c.nextRun[service.Name]
		if !ok || !now.Before(nextRun) {
			due = append(due, service)
			c.nextRun[service.Name] = now.Add(service.Interval)
		}
	}
	for name := range c.results {
		if _, ok := seen[name]; !ok {
			delete(c.results, name)
			delete(c.states, name)
			delete(c.nextRun, name)
		}
	}
	c.mu.Unlock()

	changed := false
	if len(due) > 0 {
		type checked struct {
			result payload.ServiceCheckResult
		}

		limit := 4
		if len(due) < limit {
			limit = len(due)
		}
		semaphore := make(chan struct{}, limit)
		results := make(chan checked, len(due))
		var wait sync.WaitGroup

		for _, service := range due {
			service := service
			wait.Add(1)
			go func() {
				defer wait.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				results <- checked{result: c.checkService(ctx, service)}
			}()
		}

		wait.Wait()
		close(results)

		c.mu.Lock()
		for item := range results {
			result := item.result
			state := c.states[result.Name]
			if result.Success {
				if !state.lastSuccess {
					state.lastTransitionAt = result.CheckedAt
				}
				state.lastSuccess = true
				state.consecutiveSuccesses++
				state.consecutiveFailures = 0
			} else {
				if state.lastSuccess || state.lastTransitionAt.IsZero() {
					state.lastTransitionAt = result.CheckedAt
				}
				state.lastSuccess = false
				state.consecutiveFailures++
				state.consecutiveSuccesses = 0
			}
			result.ConsecutiveSuccesses = state.consecutiveSuccesses
			result.ConsecutiveFailures = state.consecutiveFailures
			result.LastTransitionAt = state.lastTransitionAt
			c.results[result.Name] = result
			c.states[result.Name] = state
		}
		c.mu.Unlock()
		changed = true
	}

	snapshot := c.snapshotResults()
	return snapshot, changed
}

func (c *ServiceCollector) checkService(ctx context.Context, service config.ServiceRuntime) payload.ServiceCheckResult {
	startedAt := time.Now().UTC()
	result := payload.ServiceCheckResult{
		Name:      service.Name,
		Type:      service.Type,
		Target:    service.Target,
		Status:    "failing",
		CheckedAt: startedAt,
	}

	ctx, cancel := context.WithTimeout(ctx, service.Timeout)
	defer cancel()

	var err error
	switch service.Type {
	case "http":
		result, err = c.checkHTTP(ctx, service, result)
	case "tcp":
		result, err = c.checkTCP(ctx, service, result)
	default:
		err = fmt.Errorf("unsupported service type %q", service.Type)
	}

	if err != nil {
		result.Success = false
		result.Status = "failing"
		result.Message = truncateString(err.Error(), 240)
	}
	return result
}

func (c *ServiceCollector) checkHTTP(ctx context.Context, service config.ServiceRuntime, result payload.ServiceCheckResult) (payload.ServiceCheckResult, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   service.Timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		TLSHandshakeTimeout:   service.Timeout,
		ResponseHeaderTimeout: service.Timeout,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: service.InsecureSkipVerify,
		},
	}
	client := &http.Client{
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, service.Method, service.Target, nil)
	if err != nil {
		return result, err
	}
	for key, value := range service.Headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	result.ResponseTimeMS = time.Since(start).Milliseconds()
	result.HTTPStatusCode = resp.StatusCode

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return result, err
	}
	bodyText := string(body)

	if !statusExpected(resp.StatusCode, service.ExpectedStatuses) {
		return result, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	if service.BodyContains != "" && !strings.Contains(bodyText, service.BodyContains) {
		return result, fmt.Errorf("response body does not contain %q", service.BodyContains)
	}
	if service.BodyNotContains != "" && strings.Contains(bodyText, service.BodyNotContains) {
		return result, fmt.Errorf("response body unexpectedly contains %q", service.BodyNotContains)
	}

	result.Success = true
	result.Status = "passing"
	result.Message = "ok"
	return result, nil
}

func (c *ServiceCollector) checkTCP(ctx context.Context, service config.ServiceRuntime, result payload.ServiceCheckResult) (payload.ServiceCheckResult, error) {
	dialer := &net.Dialer{Timeout: service.Timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", service.Target)
	if err != nil {
		return result, err
	}
	defer conn.Close()

	result.ResponseTimeMS = time.Since(start).Milliseconds()
	result.Success = true
	result.Status = "passing"
	result.Message = "port reachable"
	return result, nil
}

func (c *ServiceCollector) snapshotResults() []payload.ServiceCheckResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	results := make([]payload.ServiceCheckResult, 0, len(c.results))
	for _, result := range c.results {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

func statusExpected(statusCode int, expected []int) bool {
	if len(expected) == 0 {
		return statusCode >= 200 && statusCode < 300
	}
	for _, code := range expected {
		if code == statusCode {
			return true
		}
	}
	return false
}

func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
