package collector

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type ProcessCollector struct {
	mu      sync.Mutex
	prevCPU map[int32]processCPUSnapshot
	lastAt  time.Time
}

type processCPUSnapshot struct {
	totalCPU float64
}

func NewProcessCollector() *ProcessCollector {
	return &ProcessCollector{
		prevCPU: map[int32]processCPUSnapshot{},
	}
}

func (c *ProcessCollector) Collect(ctx context.Context, cfg *config.Runtime) ([]payload.ProcessStat, []string) {
	if !cfg.Processes.Enabled {
		return nil, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, []string{"failed to list processes: " + err.Error()}
	}

	now := time.Now().UTC()
	elapsed := now.Sub(c.lastAt)
	if c.lastAt.IsZero() || elapsed <= 0 {
		elapsed = 0
	}

	results := make([]payload.ProcessStat, 0, len(processes))
	skipped := 0
	for _, proc := range processes {
		stat, ok := c.collectProcess(ctx, proc, cfg, elapsed)
		if !ok {
			skipped++
			continue
		}
		results = append(results, stat)
	}

	c.prevCPU = rebuildProcessCPUMap(ctx, processes)
	c.lastAt = now

	sort.Slice(results, func(i, j int) bool {
		if results[i].CPUPercent == results[j].CPUPercent {
			if results[i].MemoryRSSBytes == results[j].MemoryRSSBytes {
				return results[i].PID < results[j].PID
			}
			return results[i].MemoryRSSBytes > results[j].MemoryRSSBytes
		}
		return results[i].CPUPercent > results[j].CPUPercent
	})

	maxProcesses := cfg.Processes.MaxProcesses
	if maxProcesses > 0 && len(results) > maxProcesses {
		results = results[:maxProcesses]
	}

	var warnings []string
	if skipped > 0 {
		warnings = append(warnings, "some short-lived or restricted processes could not be sampled")
	}
	return results, warnings
}

func (c *ProcessCollector) collectProcess(ctx context.Context, proc *process.Process, cfg *config.Runtime, elapsed time.Duration) (payload.ProcessStat, bool) {
	name, err := proc.NameWithContext(ctx)
	if err != nil {
		return payload.ProcessStat{}, false
	}

	executable, _ := proc.ExeWithContext(ctx)
	command, _ := proc.CmdlineWithContext(ctx)
	if !matchesProcessFilters(name, executable, command, cfg.Processes.IncludePatterns, cfg.Processes.ExcludePatterns) {
		return payload.ProcessStat{}, false
	}

	memoryInfo, err := proc.MemoryInfoWithContext(ctx)
	if err != nil {
		return payload.ProcessStat{}, false
	}

	cpuPercent := 0.0
	if times, err := proc.TimesWithContext(ctx); err == nil {
		totalCPU := processCPUTime(times)
		if prev, ok := c.prevCPU[proc.Pid]; ok && elapsed > 0 {
			cpuPercent = round2((totalCPU - prev.totalCPU) / elapsed.Seconds() * 100)
			if cpuPercent < 0 {
				cpuPercent = 0
			}
		}
	}

	username, _ := proc.UsernameWithContext(ctx)
	threadCount, _ := proc.NumThreadsWithContext(ctx)
	createTime, _ := proc.CreateTimeWithContext(ctx)
	ppid, _ := proc.PpidWithContext(ctx)
	status, _ := proc.StatusWithContext(ctx)

	result := payload.ProcessStat{
		PID:            proc.Pid,
		ParentPID:      ppid,
		Name:           name,
		Status:         processStatusString(status),
		Username:       username,
		CPUPercent:     cpuPercent,
		MemoryRSSBytes: memoryInfo.RSS,
		MemoryVMSBytes: memoryInfo.VMS,
		ThreadCount:    threadCount,
		StartedAt:      timeFromUnixMillis(createTime),
	}
	if cfg.Processes.IncludeExecPath {
		result.Executable = executable
	}
	if cfg.Processes.IncludeCommand {
		result.Command = truncateString(command, 512)
	}
	return result, true
}

func rebuildProcessCPUMap(ctx context.Context, processes []*process.Process) map[int32]processCPUSnapshot {
	nextPrev := make(map[int32]processCPUSnapshot, len(processes))
	for _, proc := range processes {
		times, err := proc.TimesWithContext(ctx)
		if err != nil {
			continue
		}
		nextPrev[proc.Pid] = processCPUSnapshot{
			totalCPU: processCPUTime(times),
		}
	}
	return nextPrev
}

func processCPUTime(times *cpu.TimesStat) float64 {
	if times == nil {
		return 0
	}
	return times.User + times.System + times.Nice + times.Iowait + times.Irq + times.Softirq + times.Steal
}

func processStatusString(status interface{}) string {
	switch value := status.(type) {
	case string:
		return value
	case []string:
		return strings.Join(value, ",")
	default:
		return ""
	}
}

func matchesProcessFilters(name string, executable string, command string, includes []string, excludes []string) bool {
	haystacks := []string{name, executable, command}
	if len(includes) > 0 && !containsAnyNormalized(haystacks, includes) {
		return false
	}
	if len(excludes) > 0 && containsAnyNormalized(haystacks, excludes) {
		return false
	}
	return true
}
