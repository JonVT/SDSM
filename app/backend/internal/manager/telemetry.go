package manager

import (
	"context"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"sdsm/app/backend/internal/models"
)

const telemetryInterval = 5 * time.Second

// StartTelemetryMonitor launches a background sampler that refreshes host and server metrics.
func (m *Manager) StartTelemetryMonitor() {
	if m == nil {
		return
	}
	m.telemetryMu.Lock()
	if m.telemetryStop != nil {
		m.telemetryMu.Unlock()
		return
	}
	stop := make(chan struct{})
	m.telemetryStop = stop
	m.telemetryMu.Unlock()

	m.telemetryWG.Add(1)
	go func() {
		defer m.telemetryWG.Done()
		ticker := time.NewTicker(telemetryInterval)
		defer ticker.Stop()
		ctx := context.Background()
		m.refreshTelemetry(ctx)
		for {
			select {
			case <-ticker.C:
				m.refreshTelemetry(ctx)
			case <-stop:
				return
			}
		}
	}()
}

// StopTelemetryMonitor stops the background telemetry sampler and waits for shutdown.
func (m *Manager) StopTelemetryMonitor() {
	if m == nil {
		return
	}
	m.telemetryMu.Lock()
	stop := m.telemetryStop
	m.telemetryStop = nil
	m.telemetryMu.Unlock()
	if stop != nil {
		close(stop)
	}
	m.telemetryWG.Wait()
}

func (m *Manager) refreshTelemetry(ctx context.Context) {
	if m == nil {
		return
	}
	snapshot, hostDelta, memTotal, diskUsage := m.collectSystemTelemetry(ctx)
	if snapshot != nil {
		m.telemetryMu.Lock()
		m.systemTelemetry = snapshot
		m.telemetryMu.Unlock()
	}
	m.refreshServerTelemetry(ctx, hostDelta, memTotal, diskUsage)
}

func (m *Manager) collectSystemTelemetry(ctx context.Context) (*models.SystemTelemetry, float64, uint64, *disk.UsageStat) {
	timesStats, err := cpu.TimesWithContext(ctx, false)
	if err != nil || len(timesStats) == 0 {
		return nil, 0, 0, nil
	}
	total := cpuTotal(timesStats[0])
	idle := timesStats[0].Idle + timesStats[0].Iowait
	deltaTotal, deltaIdle, hasPrev := m.updateCPUSample(total, idle)

	var cpuPercent float64
	if hasPrev && deltaTotal > 0 {
		used := deltaTotal - deltaIdle
		if used < 0 {
			used = 0
		}
		cpuPercent = clampFloat((used/deltaTotal)*100, 0, 100)
	}

	memoryStats, _ := mem.VirtualMemoryWithContext(ctx)
	var memPercent float64
	var memUsed, memTotal uint64
	if memoryStats != nil {
		memPercent = clampFloat(memoryStats.UsedPercent, 0, 100)
		memUsed = memoryStats.Used
		memTotal = memoryStats.Total
	}

	rootPath := "/"
	if m.Paths != nil && strings.TrimSpace(m.Paths.RootPath) != "" {
		rootPath = m.Paths.RootPath
	}
	diskStats, _ := disk.UsageWithContext(ctx, rootPath)
	var diskPercent float64
	var diskUsed, diskTotal uint64
	if diskStats != nil {
		diskPercent = clampFloat(diskStats.UsedPercent, 0, 100)
		diskUsed = diskStats.Used
		diskTotal = diskStats.Total
	}

	ioCounters, _ := net.IOCountersWithContext(ctx, true)
	var netRecv, netSent uint64
	netInterfaces := len(ioCounters)
	for _, ctr := range ioCounters {
		netRecv += ctr.BytesRecv
		netSent += ctr.BytesSent
	}

	loadStats, _ := load.AvgWithContext(ctx)
	var load1, load5, load15 float64
	if loadStats != nil {
		load1 = loadStats.Load1
		load5 = loadStats.Load5
		load15 = loadStats.Load15
	}

	hostInfo, _ := host.InfoWithContext(ctx)
	var uptimeSeconds, processCount uint64
	if hostInfo != nil {
		uptimeSeconds = hostInfo.Uptime
		processCount = hostInfo.Procs
	}

	sampledAt := time.Now()
	netInRate, netOutRate := m.computeNetworkRates(netRecv, netSent, sampledAt)

	health := computeHealth(cpuPercent, memPercent, diskPercent)
	snapshot := &models.SystemTelemetry{
		CPUPercent:    cpuPercent,
		MemoryPercent: memPercent,
		MemoryUsed:    memUsed,
		MemoryTotal:   memTotal,
		DiskPercent:   diskPercent,
		DiskUsed:      diskUsed,
		DiskTotal:     diskTotal,
		NetworkInboundBytes:   netRecv,
		NetworkOutboundBytes:  netSent,
		NetworkInboundBps:     netInRate,
		NetworkOutboundBps:    netOutRate,
		NetworkInterfaces:     netInterfaces,
		Load1:                 load1,
		Load5:                 load5,
		Load15:                load15,
		UptimeSeconds:         uptimeSeconds,
		ProcessCount:          processCount,
		HealthPercent: health,
		SampledAt:     sampledAt,
	}

	return snapshot, deltaTotal, memTotal, diskStats
}

func (m *Manager) refreshServerTelemetry(ctx context.Context, hostDelta float64, memTotal uint64, diskStats *disk.UsageStat) {
	if m == nil {
		return
	}
	for _, srv := range m.Servers {
		if srv == nil {
			continue
		}
		usage := m.buildServerUsage(ctx, srv, hostDelta, memTotal, diskStats)
		srv.UpdateResourceUsage(usage)
	}
}

func (m *Manager) buildServerUsage(ctx context.Context, srv *models.Server, hostDelta float64, memTotal uint64, diskStats *disk.UsageStat) *models.ServerResourceUsage {
	if srv == nil {
		return nil
	}
	if !srv.IsRunning() {
		m.clearProcessSample(srv.ID)
		return nil
	}
	pid := srv.PID()
	if pid <= 0 {
		m.clearProcessSample(srv.ID)
		return nil
	}
	proc, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		m.clearProcessSample(srv.ID)
		return nil
	}
	timesStat, err := proc.TimesWithContext(ctx)
	if err != nil {
		m.clearProcessSample(srv.ID)
		return nil
	}
	total := timesStat.Total()
	cpuPercent := m.computeProcessCPUPercent(srv.ID, total, hostDelta)

	memInfo, err := proc.MemoryInfoWithContext(ctx)
	var rss uint64
	var memPercent float64
	if err == nil && memInfo != nil {
		rss = memInfo.RSS
		if memTotal > 0 {
			memPercent = clampFloat((float64(rss)/float64(memTotal))*100, 0, 100)
		}
	}

	var diskPercent float64
	var diskUsed uint64
	var volume string
	if diskStats != nil {
		diskPercent = clampFloat(diskStats.UsedPercent, 0, 100)
		diskUsed = diskStats.Used
		volume = diskStats.Path
	}

	now := time.Now()
	return &models.ServerResourceUsage{
		CPUPercent:       cpuPercent,
		MemoryPercent:    memPercent,
		MemoryRSSBytes:   rss,
		DiskUsageBytes:   diskUsed,
		DiskPercent:      diskPercent,
		SampledAt:        now,
		DiskSampledAt:    now,
		VolumeMountPoint: volume,
	}
}

func (m *Manager) computeProcessCPUPercent(serverID int, total, hostDelta float64) float64 {
	prev := m.storeProcessSample(serverID, total)
	if prev == 0 || hostDelta <= 0 {
		return 0
	}
	delta := total - prev
	if delta <= 0 {
		return 0
	}
	pct := (delta / hostDelta) * 100
	return clampFloat(pct, 0, float64(runtime.NumCPU())*100)
}

func (m *Manager) storeProcessSample(serverID int, total float64) float64 {
	m.telemetryMu.Lock()
	defer m.telemetryMu.Unlock()
	prev := m.serverCPUTimes[serverID]
	m.serverCPUTimes[serverID] = total
	return prev
}

func (m *Manager) clearProcessSample(serverID int) {
	m.telemetryMu.Lock()
	delete(m.serverCPUTimes, serverID)
	m.telemetryMu.Unlock()
}

func cpuTotal(stat cpu.TimesStat) float64 {
	return stat.User + stat.System + stat.Nice + stat.Idle + stat.Iowait + stat.Irq + stat.Softirq + stat.Steal + stat.Guest + stat.GuestNice
}

func (m *Manager) updateCPUSample(total, idle float64) (float64, float64, bool) {
	m.telemetryMu.Lock()
	defer m.telemetryMu.Unlock()
	deltaTotal := total - m.lastCPUTotal
	deltaIdle := idle - m.lastCPUIdle
	hasPrev := m.lastCPUTotal > 0
	m.lastCPUTotal = total
	m.lastCPUIdle = idle
	return deltaTotal, deltaIdle, hasPrev
}

func (m *Manager) computeNetworkRates(recv, sent uint64, now time.Time) (float64, float64) {
	if m == nil {
		return 0, 0
	}
	m.telemetryMu.Lock()
	defer m.telemetryMu.Unlock()
	var inbound, outbound float64
	if !m.lastNetSample.IsZero() && now.After(m.lastNetSample) {
		elapsed := now.Sub(m.lastNetSample).Seconds()
		if elapsed > 0 {
			if recv >= m.lastNetRecv {
				inbound = float64(recv-m.lastNetRecv) / elapsed
			}
			if sent >= m.lastNetSent {
				outbound = float64(sent-m.lastNetSent) / elapsed
			}
		}
	}
	m.lastNetRecv = recv
	m.lastNetSent = sent
	m.lastNetSample = now
	return inbound, outbound
}

func computeHealth(cpu, mem, disk float64) float64 {
	maxUsage := 0.0
	for _, v := range []float64{cpu, mem, disk} {
		if v <= 0 {
			continue
		}
		if v > maxUsage {
			maxUsage = v
		}
	}
	if maxUsage == 0 {
		return 100
	}
	health := 100 - maxUsage
	return clampFloat(health, 0, 100)
}

func clampFloat(val, min, max float64) float64 {
	if math.IsNaN(val) {
		return min
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// SystemTelemetry returns the last sampled host metrics snapshot.
func (m *Manager) SystemTelemetry() *models.SystemTelemetry {
	if m == nil {
		return nil
	}
	m.telemetryMu.RLock()
	defer m.telemetryMu.RUnlock()
	if m.systemTelemetry == nil {
		return nil
	}
	copy := *m.systemTelemetry
	return &copy
}

// SystemHealthPercent returns the most recent health score (0-100).
func (m *Manager) SystemHealthPercent() float64 {
	telemetry := m.SystemTelemetry()
	if telemetry == nil {
		return 100
	}
	return clampFloat(telemetry.HealthPercent, 0, 100)
}
