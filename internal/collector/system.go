package collector

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type SystemCollector struct {
	mu         sync.Mutex
	prevNet    map[string]counterSnapshot
	prevDiskIO map[string]counterSnapshot
}

type counterSnapshot struct {
	at         time.Time
	readBytes  uint64
	writeBytes uint64
}

func NewSystemCollector() *SystemCollector {
	return &SystemCollector{
		prevNet:    map[string]counterSnapshot{},
		prevDiskIO: map[string]counterSnapshot{},
	}
}

func (c *SystemCollector) Collect(ctx context.Context, cfg *config.Runtime) (payload.SystemMetrics, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UTC()
	metrics := payload.SystemMetrics{CollectedAt: now}
	warnings := make([]string, 0, 6)

	if hostInfo, err := host.InfoWithContext(ctx); err == nil {
		metrics.UptimeSeconds = hostInfo.Uptime
		metrics.Host = payload.HostInfo{
			Hostname:             hostInfo.Hostname,
			Platform:             hostInfo.Platform,
			PlatformVersion:      hostInfo.PlatformVersion,
			KernelVersion:        hostInfo.KernelVersion,
			KernelArch:           hostInfo.KernelArch,
			VirtualizationRole:   hostInfo.VirtualizationRole,
			VirtualizationSystem: hostInfo.VirtualizationSystem,
			BootTime:             timeFromUnixSeconds(hostInfo.BootTime),
		}
	} else {
		warnings = append(warnings, "failed to collect host metadata: "+err.Error())
		metrics.Host.Hostname = cfg.Hostname
	}

	if usage, err := cpu.PercentWithContext(ctx, cfg.Collection.CPUSampleInterval, false); err == nil && len(usage) > 0 {
		metrics.CPU.UsagePercent = round2(usage[0])
	} else if err != nil {
		warnings = append(warnings, "failed to sample CPU usage: "+err.Error())
	}

	if logical, err := cpu.CountsWithContext(ctx, true); err == nil {
		metrics.CPU.LogicalCores = logical
	}
	if physical, err := cpu.CountsWithContext(ctx, false); err == nil {
		metrics.CPU.PhysicalCores = physical
	}
	metrics.CPU.SampleWindowMS = cfg.Collection.CPUSampleInterval.Milliseconds()

	if avg, err := load.AvgWithContext(ctx); err == nil {
		metrics.Load = payload.LoadStats{
			Load1:  round2(avg.Load1),
			Load5:  round2(avg.Load5),
			Load15: round2(avg.Load15),
		}
	} else {
		warnings = append(warnings, "failed to read load averages: "+err.Error())
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		metrics.Memory = payload.MemoryStats{
			TotalBytes:     vm.Total,
			AvailableBytes: vm.Available,
			UsedBytes:      vm.Used,
			FreeBytes:      vm.Free,
			UsedPercent:    round2(vm.UsedPercent),
		}
	} else {
		warnings = append(warnings, "failed to read memory metrics: "+err.Error())
	}

	if swap, err := mem.SwapMemoryWithContext(ctx); err == nil {
		metrics.Swap = payload.SwapStats{
			TotalBytes:  swap.Total,
			UsedBytes:   swap.Used,
			FreeBytes:   swap.Free,
			UsedPercent: round2(swap.UsedPercent),
		}
	} else {
		warnings = append(warnings, "failed to read swap metrics: "+err.Error())
	}

	metrics.Disks = c.collectDiskUsage(ctx, &warnings)
	if cfg.Collection.IncludeDiskIO {
		metrics.DiskIO = c.collectDiskIO(ctx, now, &warnings)
	}
	metrics.Network = c.collectNetwork(ctx, cfg.Collection.IncludeConnections, now, &warnings)
	return metrics, warnings
}

func (c *SystemCollector) collectDiskUsage(ctx context.Context, warnings *[]string) []payload.DiskUsage {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		*warnings = append(*warnings, "failed to list disk partitions: "+err.Error())
		return nil
	}

	results := make([]payload.DiskUsage, 0, len(partitions))
	seenMounts := map[string]struct{}{}
	skipped := 0
	for _, partition := range partitions {
		if partition.Mountpoint == "" || ignoreFSType(partition.Fstype) {
			continue
		}
		if _, ok := seenMounts[partition.Mountpoint]; ok {
			continue
		}
		seenMounts[partition.Mountpoint] = struct{}{}
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil {
			skipped++
			continue
		}
		results = append(results, payload.DiskUsage{
			Device:      partition.Device,
			Mountpoint:  partition.Mountpoint,
			FSType:      partition.Fstype,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: round2(usage.UsedPercent),
		})
	}

	if skipped > 0 {
		*warnings = append(*warnings, "some disk mountpoints could not be sampled")
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Mountpoint < results[j].Mountpoint
	})
	return results
}

func (c *SystemCollector) collectDiskIO(ctx context.Context, now time.Time, warnings *[]string) []payload.DeviceIOStats {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		*warnings = append(*warnings, "failed to read disk I/O counters: "+err.Error())
		return nil
	}

	results := make([]payload.DeviceIOStats, 0, len(counters))
	nextPrev := make(map[string]counterSnapshot, len(counters))
	for device, stat := range counters {
		prev := c.prevDiskIO[device]
		readRate := ratePerSecond(prev.readBytes, stat.ReadBytes, prev.at, now)
		writeRate := ratePerSecond(prev.writeBytes, stat.WriteBytes, prev.at, now)
		results = append(results, payload.DeviceIOStats{
			Device:           device,
			ReadBytes:        stat.ReadBytes,
			WriteBytes:       stat.WriteBytes,
			ReadOps:          stat.ReadCount,
			WriteOps:         stat.WriteCount,
			ReadBytesPerSec:  round2(readRate),
			WriteBytesPerSec: round2(writeRate),
		})
		nextPrev[device] = counterSnapshot{
			at:         now,
			readBytes:  stat.ReadBytes,
			writeBytes: stat.WriteBytes,
		}
	}
	c.prevDiskIO = nextPrev

	sort.Slice(results, func(i, j int) bool {
		return results[i].Device < results[j].Device
	})
	return results
}

func (c *SystemCollector) collectNetwork(ctx context.Context, includeConnections bool, now time.Time, warnings *[]string) payload.NetworkStats {
	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		*warnings = append(*warnings, "failed to read network counters: "+err.Error())
		return payload.NetworkStats{}
	}

	results := make([]payload.InterfaceIO, 0, len(counters))
	nextPrev := make(map[string]counterSnapshot, len(counters))
	var totalSent uint64
	var totalRecv uint64
	var totalSentRate float64
	var totalRecvRate float64

	for _, stat := range counters {
		if stat.Name == "" {
			continue
		}
		prev := c.prevNet[stat.Name]
		sentRate := ratePerSecond(prev.writeBytes, stat.BytesSent, prev.at, now)
		recvRate := ratePerSecond(prev.readBytes, stat.BytesRecv, prev.at, now)
		results = append(results, payload.InterfaceIO{
			Name:        stat.Name,
			BytesSent:   stat.BytesSent,
			BytesRecv:   stat.BytesRecv,
			PacketsSent: stat.PacketsSent,
			PacketsRecv: stat.PacketsRecv,
			Errin:       stat.Errin,
			Errout:      stat.Errout,
			Dropin:      stat.Dropin,
			Dropout:     stat.Dropout,
			SentPerSec:  round2(sentRate),
			RecvPerSec:  round2(recvRate),
		})
		nextPrev[stat.Name] = counterSnapshot{
			at:         now,
			readBytes:  stat.BytesRecv,
			writeBytes: stat.BytesSent,
		}
		totalSent += stat.BytesSent
		totalRecv += stat.BytesRecv
		totalSentRate += sentRate
		totalRecvRate += recvRate
	}
	c.prevNet = nextPrev

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	networkStats := payload.NetworkStats{
		Interfaces:      results,
		TotalBytesSent:  totalSent,
		TotalBytesRecv:  totalRecv,
		TotalSentPerSec: round2(totalSentRate),
		TotalRecvPerSec: round2(totalRecvRate),
	}
	if includeConnections {
		connections, err := gnet.ConnectionsWithContext(ctx, "inet")
		if err != nil {
			*warnings = append(*warnings, "failed to read active network connections: "+err.Error())
		} else {
			networkStats.ConnectionCount = len(connections)
		}
	}
	return networkStats
}

func ignoreFSType(fsType string) bool {
	switch strings.ToLower(strings.TrimSpace(fsType)) {
	case "", "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "squashfs", "overlay", "nsfs", "cgroup", "cgroup2", "tracefs", "securityfs", "pstore", "autofs", "hugetlbfs", "mqueue":
		return true
	default:
		return false
	}
}

func ratePerSecond(previous uint64, current uint64, previousAt time.Time, currentAt time.Time) float64 {
	if previousAt.IsZero() || current < previous {
		return 0
	}
	elapsed := currentAt.Sub(previousAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(current-previous) / elapsed
}
