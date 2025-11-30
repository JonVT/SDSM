package manager

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SetupStageProgress represents parsed progress for a single deployment stage
type SetupStageProgress struct {
	Component   string    `json:"component"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"` // Pending | Running | Completed | Error
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	DurationMS  int64     `json:"duration_ms"`
	Percent     int       `json:"percent"`
	LastLine    string    `json:"last_line,omitempty"`
}

// SetupLogProgress aggregates overall progress parsed from updates.log
type SetupLogProgress struct {
	InProgress     bool                 `json:"in_progress"`
	OverallPercent int                  `json:"overall_percent"`
	Stages         []SetupStageProgress `json:"stages"`
	LastUpdated    time.Time            `json:"last_updated"`
}

var (
	tsPrefix       = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}):\s+`)
	startedRe      = regexp.MustCompile(`^Deployment \(([^)]+)\) started$`)
	completedOKRe  = regexp.MustCompile(`^Deployment \(([^)]+)\) completed successfully in ([0-9a-zA-Z\.:]+)$`)
	completedErrRe = regexp.MustCompile(`^Deployment \(([^)]+)\) completed with errors in ([0-9a-zA-Z\.:]+)$`)
	// Only treat Steam app download progress lines as progress when the state is 0x61 (downloading)
	steamStateDownloadRe = regexp.MustCompile(`Update state \(0x61\)\s+downloading, progress:\s*([0-9]+(?:\.[0-9]+)?)\s*\(`)
)

// ParseSetupProgressFromUpdateLog scans the update log and returns structured progress.
func (m *Manager) ParseSetupProgressFromUpdateLog() SetupLogProgress {
	res := SetupLogProgress{InProgress: m.IsUpdating(), OverallPercent: 0, LastUpdated: time.Now()}
	// Initialize stage map in the preferred reporting order
	// For setup progress, also include SERVERS as a final stage.
	order := append([]DeployType{}, progressOrder...)
	order = append(order, DeployTypeServers)
	stageMap := make(map[DeployType]*SetupStageProgress)
	for _, dt := range order {
		stageMap[dt] = &SetupStageProgress{
			Component:   string(dt),
			DisplayName: dt.displayName(),
			Status:      "Pending",
			Percent:     0,
		}
	}

	logPath := m.Paths.UpdateLogFile()
	file, err := os.Open(logPath)
	if err != nil {
		// If the file can't be opened, return defaults
		// (avoid importing utils just to log here)
		res.Stages = stagesFromMap(order, stageMap)
		res.OverallPercent = computeOverallPercent(res.Stages)
		return res
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lastTS time.Time
	var currentComponent DeployType
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Extract timestamp
		ts := lastTS
		if m := tsPrefix.FindStringSubmatch(line); len(m) == 2 {
			if t, err := time.Parse("2006-01-02 15:04:05", m[1]); err == nil {
				ts = t
				lastTS = t
			}
			// Remove timestamp prefix for content parsing
			line = tsPrefix.ReplaceAllString(line, "")
		}

		if mm := startedRe.FindStringSubmatch(line); len(mm) == 2 {
			comp := DeployType(strings.ToUpper(strings.TrimSpace(mm[1])))
			if sp, ok := stageMap[comp]; ok {
				sp.Status = "Running"
				sp.StartedAt = ts
				sp.Percent = 0
				sp.LastLine = "started"
				currentComponent = comp
			}
			continue
		}

		if mm := completedOKRe.FindStringSubmatch(line); len(mm) == 3 {
			comp := DeployType(strings.ToUpper(strings.TrimSpace(mm[1])))
			if sp, ok := stageMap[comp]; ok {
				sp.Status = "Completed"
				sp.CompletedAt = ts
				sp.DurationMS = parseDurationMillis(mm[2])
				if sp.Percent < 100 {
					sp.Percent = 100
				}
				sp.LastLine = "completed"
				if currentComponent == comp {
					currentComponent = ""
				}
			}
			continue
		}

		if mm := completedErrRe.FindStringSubmatch(line); len(mm) == 3 {
			comp := DeployType(strings.ToUpper(strings.TrimSpace(mm[1])))
			if sp, ok := stageMap[comp]; ok {
				sp.Status = "Error"
				sp.CompletedAt = ts
				sp.DurationMS = parseDurationMillis(mm[2])
				if sp.Percent < 100 {
					// leave percent as-is to reflect where it failed
				}
				sp.LastLine = "error"
				if currentComponent == comp {
					currentComponent = ""
				}
			}
			continue
		}

		// Capture Steam app download progress into RELEASE/BETA only for downloading state (0x61)
		if currentComponent == DeployTypeRelease || currentComponent == DeployTypeBeta {
			if mm := steamStateDownloadRe.FindStringSubmatch(line); len(mm) == 2 {
				if p := parseFloatPercent(mm[1]); p >= 0 {
					if sp, ok := stageMap[currentComponent]; ok {
						sp.Status = "Running"
						sp.Percent = int(p + 0.5)
						sp.LastLine = "progress"
						sp.CompletedAt = time.Time{}
					}
				}
			}
		}
	}
	// Build ordered stages and compute overall percent
	res.Stages = stagesFromMap(order, stageMap)
	// Inferred in-progress if any stage is Running
	for _, s := range res.Stages {
		if s.Status == "Running" {
			res.InProgress = true
			break
		}
	}
	res.OverallPercent = computeOverallPercent(res.Stages)
	if !lastTS.IsZero() {
		res.LastUpdated = lastTS
	}
	return res
}

func stagesFromMap(order []DeployType, m map[DeployType]*SetupStageProgress) []SetupStageProgress {
	out := make([]SetupStageProgress, 0, len(order))
	for _, dt := range order {
		if sp, ok := m[dt]; ok && sp != nil {
			// copy value
			v := *sp
			out = append(out, v)
		}
	}
	return out
}

func computeOverallPercent(stages []SetupStageProgress) int {
	// Consider only stages that have started (Running/Completed/Error) for denominator
	started := 0
	totalPct := 0
	for _, s := range stages {
		switch s.Status {
		case "Running":
			started++
			if s.Percent > 0 {
				totalPct += clamp(s.Percent, 0, 100)
			}
		case "Completed":
			started++
			totalPct += 100
		case "Error":
			started++
			// leave as current percent
			totalPct += clamp(s.Percent, 0, 100)
		}
	}
	if started == 0 {
		return 0
	}
	pct := totalPct / started
	return clamp(pct, 0, 100)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func parseDurationMillis(s string) int64 {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return d.Milliseconds()
}

func parseFloatPercent(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	// simplistic parser to avoid importing strconv multiple times here
	// defer to strconv when available
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return -1
}
