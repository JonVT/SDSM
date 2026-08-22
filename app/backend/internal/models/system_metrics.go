package models

import "time"

// SystemTelemetry captures host-level resource usage sampled for dashboard display.
type SystemTelemetry struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	MemoryUsed    uint64    `json:"memory_used_bytes"`
	MemoryTotal   uint64    `json:"memory_total_bytes"`
	DiskPercent   float64   `json:"disk_percent"`
	DiskUsed      uint64    `json:"disk_used_bytes"`
	DiskTotal     uint64    `json:"disk_total_bytes"`
	NetworkInboundBytes   uint64    `json:"network_in_bytes"`
	NetworkOutboundBytes  uint64    `json:"network_out_bytes"`
	NetworkInboundBps     float64   `json:"network_in_bps"`
	NetworkOutboundBps    float64   `json:"network_out_bps"`
	NetworkInterfaces     int       `json:"network_interfaces"`
	Load1                 float64   `json:"load1"`
	Load5                 float64   `json:"load5"`
	Load15                float64   `json:"load15"`
	UptimeSeconds         uint64    `json:"uptime_seconds"`
	ProcessCount          uint64    `json:"process_count"`
	HealthPercent float64   `json:"health_percent"`
	SampledAt     time.Time `json:"sampled_at"`
}

// DashboardNotification represents a recent lifecycle/update event surfaced in the UI feed.
type DashboardNotification struct {
	ID        uint64    `json:"id"`
	Kind      string    `json:"kind"`
	Event     string    `json:"event,omitempty"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	ServerID  int       `json:"server_id,omitempty"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	NotificationKindInfo    = "info"
	NotificationKindSuccess = "success"
	NotificationKindWarning = "warning"
	NotificationKindDanger  = "danger"
)
