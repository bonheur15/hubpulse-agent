package payload

import "time"

const SchemaVersion = "hubpulse-agent/v1"

type Envelope struct {
	SchemaVersion string        `json:"schema_version"`
	Agent         AgentMetadata `json:"agent"`
	Samples       []Snapshot    `json:"samples"`
}

type AgentMetadata struct {
	ID        string            `json:"id"`
	Hostname  string            `json:"hostname"`
	Labels    map[string]string `json:"labels,omitempty"`
	Version   string            `json:"version"`
	ConfigRev string            `json:"config_revision,omitempty"`
}

type Snapshot struct {
	CapturedAt time.Time            `json:"captured_at"`
	System     *SystemMetrics       `json:"system,omitempty"`
	Processes  []ProcessStat        `json:"processes,omitempty"`
	Services   []ServiceCheckResult `json:"services,omitempty"`
	Warnings   []string             `json:"warnings,omitempty"`
}

type SystemMetrics struct {
	CollectedAt   time.Time       `json:"collected_at"`
	UptimeSeconds uint64          `json:"uptime_seconds"`
	Host          HostInfo        `json:"host"`
	CPU           CPUStats        `json:"cpu"`
	Load          LoadStats       `json:"load"`
	Memory        MemoryStats     `json:"memory"`
	Swap          SwapStats       `json:"swap"`
	Disks         []DiskUsage     `json:"disks,omitempty"`
	DiskIO        []DeviceIOStats `json:"disk_io,omitempty"`
	Network       NetworkStats    `json:"network"`
}

type HostInfo struct {
	Hostname             string    `json:"hostname"`
	Platform             string    `json:"platform,omitempty"`
	PlatformVersion      string    `json:"platform_version,omitempty"`
	KernelVersion        string    `json:"kernel_version,omitempty"`
	KernelArch           string    `json:"kernel_arch,omitempty"`
	VirtualizationSystem string    `json:"virtualization_system,omitempty"`
	VirtualizationRole   string    `json:"virtualization_role,omitempty"`
	BootTime             time.Time `json:"boot_time,omitempty"`
}

type CPUStats struct {
	UsagePercent   float64 `json:"usage_percent"`
	LogicalCores   int     `json:"logical_cores"`
	PhysicalCores  int     `json:"physical_cores"`
	SampleWindowMS int64   `json:"sample_window_ms"`
}

type LoadStats struct {
	Load1  float64 `json:"load_1"`
	Load5  float64 `json:"load_5"`
	Load15 float64 `json:"load_15"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type SwapStats struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskUsage struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	FSType      string  `json:"fs_type"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DeviceIOStats struct {
	Device           string  `json:"device"`
	ReadBytes        uint64  `json:"read_bytes"`
	WriteBytes       uint64  `json:"write_bytes"`
	ReadOps          uint64  `json:"read_ops"`
	WriteOps         uint64  `json:"write_ops"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

type NetworkStats struct {
	ConnectionCount int           `json:"connection_count"`
	Interfaces      []InterfaceIO `json:"interfaces,omitempty"`
	TotalBytesSent  uint64        `json:"total_bytes_sent"`
	TotalBytesRecv  uint64        `json:"total_bytes_recv"`
	TotalSentPerSec float64       `json:"total_sent_per_sec"`
	TotalRecvPerSec float64       `json:"total_recv_per_sec"`
}

type InterfaceIO struct {
	Name        string  `json:"name"`
	BytesSent   uint64  `json:"bytes_sent"`
	BytesRecv   uint64  `json:"bytes_recv"`
	PacketsSent uint64  `json:"packets_sent"`
	PacketsRecv uint64  `json:"packets_recv"`
	Errin       uint64  `json:"err_in"`
	Errout      uint64  `json:"err_out"`
	Dropin      uint64  `json:"drop_in"`
	Dropout     uint64  `json:"drop_out"`
	SentPerSec  float64 `json:"sent_per_sec"`
	RecvPerSec  float64 `json:"recv_per_sec"`
}

type ProcessStat struct {
	PID            int32     `json:"pid"`
	ParentPID      int32     `json:"parent_pid"`
	Name           string    `json:"name"`
	Executable     string    `json:"executable,omitempty"`
	Command        string    `json:"command,omitempty"`
	Status         string    `json:"status,omitempty"`
	Username       string    `json:"username,omitempty"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryRSSBytes uint64    `json:"memory_rss_bytes"`
	MemoryVMSBytes uint64    `json:"memory_vms_bytes"`
	ThreadCount    int32     `json:"thread_count"`
	StartedAt      time.Time `json:"started_at,omitempty"`
}

type ServiceCheckResult struct {
	Name                 string    `json:"name"`
	Type                 string    `json:"type"`
	Target               string    `json:"target"`
	Status               string    `json:"status"`
	Success              bool      `json:"success"`
	CheckedAt            time.Time `json:"checked_at"`
	ResponseTimeMS       int64     `json:"response_time_ms"`
	HTTPStatusCode       int       `json:"http_status_code,omitempty"`
	Message              string    `json:"message,omitempty"`
	ConsecutiveSuccesses int       `json:"consecutive_successes"`
	ConsecutiveFailures  int       `json:"consecutive_failures"`
	LastTransitionAt     time.Time `json:"last_transition_at,omitempty"`
}
