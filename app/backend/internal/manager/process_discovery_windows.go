//go:build windows

package manager

import (
	"context"
	wmi "github.com/StackExchange/wmi"
	"path/filepath"
	"regexp"
	"sdsm/app/backend/internal/utils"
	"strconv"
	"strings"
	"time"
)

// discoverRunningServerPIDs on Windows currently returns an empty map and relies on
// the existing PID file mechanism for detached server discovery. A future enhancement
// can leverage Toolhelp32Snapshot or WMI/CIM queries to locate running processes by
// command line and match -logFile arguments similar to the Unix implementation.
type win32Process struct {
	Name        string  // e.g., rocketstation_DedicatedServer.exe
	ProcessID   uint32  // PID
	CommandLine *string // may be nil
}

func discoverRunningServerPIDs(paths *utils.Paths, wmiEnabled bool, logf func(string)) map[int]int {
	result := make(map[int]int)
	if paths == nil {
		return result
	}
	// Allow disabling WMI-based discovery via configuration for environments where WMI is blocked/disabled.
	if !wmiEnabled {
		if logf != nil {
			logf("Process discovery (windows): WMI disabled by configuration; relying on PID files")
		}
		return result
	}

	if logf != nil {
		logf("Process discovery (windows): querying WMI for rocketstation_DedicatedServer.exe")
	}
	// Perform WMI query with timeout and simple retries to harden against transient issues.
	var procs []win32Process
	q := "SELECT Name, ProcessId, CommandLine FROM Win32_Process WHERE Name='rocketstation_DedicatedServer.exe'"
	// up to 3 attempts with backoff
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		// 3s timeout per attempt
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := wmi.QueryWithContext(ctx, q, &procs)
		cancel()
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if logf != nil {
			logf("Process discovery (windows): WMI query attempt " + strconv.Itoa(attempt) + " failed: " + err.Error())
		}
		// Backoff: 250ms, 500ms (skip sleep after last attempt)
		if attempt < 3 {
			time.Sleep(time.Duration(250*attempt) * time.Millisecond)
		}
	}
	if lastErr != nil {
		if logf != nil {
			logf("Process discovery (windows): WMI unavailable after retries; falling back to PID files: " + lastErr.Error())
		}
		return result
	}
	// Regex for ServerN_output.log
	filePattern := regexp.MustCompile(`(?i)^Server(\d+)_output\.log$`)
	for _, p := range procs {
		pid := int(p.ProcessID)
		if pid <= 0 {
			continue
		}
		if p.CommandLine == nil {
			if logf != nil {
				logf("Process discovery (windows): PID " + strconv.Itoa(pid) + " CommandLine unavailable (insufficient permissions?). Try running SDSM elevated or rely on PID files.")
			}
			continue
		}
		cmd := *p.CommandLine
		// Look for -logFile and extract following token (handles quotes/basic spacing)
		// We will split on spaces but also handle quoted segments by a simple scan.
		tokens := splitWindowsCmdline(cmd)
		var logFile string
		for i := 0; i < len(tokens); i++ {
			if strings.EqualFold(tokens[i], "-logFile") && i+1 < len(tokens) {
				logFile = strings.Trim(tokens[i+1], " \"'")
				break
			}
		}
		if logFile == "" {
			continue
		}
		if logf != nil {
			logf("Process discovery (windows): PID " + strconv.Itoa(pid) + " uses -logFile " + logFile)
		}
		base := filepath.Base(logFile)
		m := filePattern.FindStringSubmatch(base)
		if len(m) != 2 {
			continue
		}
		sid, _ := strconv.Atoi(m[1])
		if sid <= 0 {
			continue
		}
		expectedDir := paths.ServerLogsDir(sid)
		realDir := filepath.Dir(logFile)
		expEval, _ := filepath.EvalSymlinks(expectedDir)
		realEval, _ := filepath.EvalSymlinks(realDir)
		if expEval == "" {
			expEval = expectedDir
		}
		if realEval == "" {
			realEval = realDir
		}
		if !samePathInsensitive(expEval, realEval) {
			if logf != nil {
				logf("Process discovery (windows): PID " + strconv.Itoa(pid) + " log dir mismatch; expected " + expEval + ", got " + realEval + ". Skipping")
			}
			continue
		}
		if _, exists := result[sid]; !exists {
			result[sid] = pid
			if logf != nil {
				logf("Process discovery (windows): mapped Server" + m[1] + " -> PID " + strconv.Itoa(pid))
			}
		}
	}
	if logf != nil {
		if len(result) == 0 {
			logf("Process discovery (windows): no running server processes found via WMI")
		} else {
			var parts []string
			for sid, pid := range result {
				parts = append(parts, strconv.Itoa(sid)+":"+strconv.Itoa(pid))
			}
			// No need to sort strictly, but keep consistency
			logf("Process discovery (windows): found " + strconv.Itoa(len(result)) + " servers: " + strings.Join(parts, ", "))
		}
	}
	return result
}

// splitWindowsCmdline performs a light-weight split respecting simple quotes.
// It is not a full Windows parser but sufficient for our -logFile extraction.
func splitWindowsCmdline(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := rune(0)
	for _, r := range s {
		switch r {
		case '\'', '"':
			if inQuote == 0 {
				inQuote = r
			} else if inQuote == r {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case ' ', '\t':
			if inQuote != 0 {
				cur.WriteRune(r)
			} else if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
