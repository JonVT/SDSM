//go:build !windows

package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sdsm/internal/utils"
	"sort"
	"strconv"
	"strings"
)

// discoverRunningServerPIDs scans /proc for Stationeers dedicated server processes
// that were started outside the current manager (detached or previous instance).
// It returns a map of serverID -> pid. Detection strategy:
//  1. Identify processes whose executable/command line contains "rocketstation_DedicatedServer"
//     (Linux build) AND includes the flag "-logFile".
//  2. Extract the path argument following "-logFile" (absolute path expected) and match the
//     filename pattern "ServerN_output.log" where N is the server ID.
//  3. Validate the parent directory matches Paths.ServerLogsDir(N) to avoid false positives.
//
// If any step fails, the process is skipped. Best-effort only; errors are silently ignored.
func discoverRunningServerPIDs(paths *utils.Paths, _ bool, logf func(string)) map[int]int {
	result := make(map[int]int)
	if paths == nil {
		return result
	}
	if logf != nil {
		logf("Process discovery (unix): scanning /proc for Stationeers server processes")
	}
	procDir := "/proc"
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return result
	}
	// Regex to extract server ID from output log filename
	// Example: /root/Server3/logs/Server3_output.log -> 3
	filePattern := regexp.MustCompile(`(?i)^Server(\d+)_output\.log$`)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// numeric PID directories only
		pid, err := strconv.Atoi(name)
		if err != nil || pid <= 0 {
			continue
		}
		cmdlinePath := filepath.Join(procDir, name, "cmdline")
		data, err := os.ReadFile(cmdlinePath)
		if err != nil || len(data) == 0 {
			continue
		}
		// /proc/<pid>/cmdline is NUL-separated
		parts := strings.Split(string(data), "\x00")
		hasExe := false
		for _, p := range parts {
			if strings.Contains(p, "rocketstation_DedicatedServer") {
				hasExe = true
				break
			}
		}
		if !hasExe {
			continue
		}
		if logf != nil {
			logf("Process discovery: candidate PID " + name + " contains rocketstation_DedicatedServer")
		}
		// Find -logFile argument and its value
		var logFile string
		for i := 0; i < len(parts); i++ {
			if parts[i] == "-logFile" && i+1 < len(parts) {
				logFile = strings.TrimSpace(parts[i+1])
				break
			}
		}
		if logFile == "" {
			continue
		}
		if logf != nil {
			logf("Process discovery: PID " + name + " uses -logFile " + logFile)
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
		if logf != nil {
			logf("Process discovery: PID " + name + " appears to be Server" + m[1])
		}
		// Verify directory consistency
		expectedDir := paths.ServerLogsDir(sid)
		realDir := filepath.Dir(logFile)
		// Resolve symlinks for robustness
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
				logf("Process discovery: PID " + name + " log dir mismatch; expected " + expEval + ", got " + realEval + ". Skipping")
			}
			continue
		}
		// Record mapping if not already set; prefer first seen (should be unique)
		if _, exists := result[sid]; !exists {
			result[sid] = pid
			if logf != nil {
				logf("Process discovery: mapped Server" + m[1] + " -> PID " + name)
			}
		}
	}
	if logf != nil {
		// create summary list
		if len(result) == 0 {
			logf("Process discovery (unix): no running server processes found")
		} else {
			// Build a compact summary like "1:1234, 2:5678"
			var parts []string
			for sid, pid := range result {
				parts = append(parts, fmt.Sprintf("%d:%d", sid, pid))
			}
			// sort for determinism
			sort.Strings(parts)
			logf("Process discovery (unix): found " + strconv.Itoa(len(result)) + " servers: " + strings.Join(parts, ", "))
		}
	}
	return result
}

// samePathInsensitive performs a case-insensitive comparison after cleaning the path.
func samePathInsensitive(a, b string) bool {
	if a == b {
		return true
	}
	aa := filepath.Clean(strings.TrimSpace(a))
	bb := filepath.Clean(strings.TrimSpace(b))
	return strings.EqualFold(aa, bb)
}
