package models

import "time"

// ServerResourceUsage captures sampled CPU/memory/disk telemetry for a managed server.
type ServerResourceUsage struct {
	CPUPercent       float64   `json:"cpu_percent"`
	MemoryPercent    float64   `json:"memory_percent"`
	MemoryRSSBytes   uint64    `json:"memory_rss_bytes"`
	DiskUsageBytes   uint64    `json:"disk_usage_bytes"`
	DiskPercent      float64   `json:"disk_percent"`
	SampledAt        time.Time `json:"sampled_at"`
	DiskSampledAt    time.Time `json:"disk_sampled_at"`
	VolumeMountPoint string    `json:"volume_mount_point"`
}

// Copy returns a deep copy of the usage snapshot so callers can mutate safely.
func (u *ServerResourceUsage) Copy() *ServerResourceUsage {
	if u == nil {
		return nil
	}
	dup := *u
	return &dup
}
