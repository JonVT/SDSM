// Package steam integrates with SteamCMD and related GitHub releases to
// download and deploy Stationeers, BepInEx, and LaunchPad components.
package steam

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"sdsm/internal/utils"
)

const (
	STEAMCMD_WIN_URL     = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip"
	STEAMCMD_LINUX_URL   = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"
	bepInExLatestAPI     = "https://api.github.com/repos/BepInEx/BepInEx/releases/latest"
	launchPadAPI         = "https://api.github.com/repos/StationeersLaunchPad/StationeersLaunchPad/releases/latest"
	launchPadFallback    = "https://github.com/StationeersLaunchPad/StationeersLaunchPad/archive/refs/heads/main.zip"
	bepInExVersionFile   = "bepinex.version"
	launchPadVersionFile = "launchpad.version"
	sconVersionFile      = "scon.version"
	sconDefaultRepo      = "JonVT/SCON" // default guess; can be overridden via configuration
)

// Steam provides helpers to query Steam APIs and deploy/update components
// such as SteamCMD, Stationeers (release/beta), BepInEx, and LaunchPad.
type Steam struct {
	SteamID           string
	Logger            *utils.Logger
	Paths             *utils.Paths
	progressReporter  func(component, stage string, downloaded, total int64)
	progressComponent string
	// Optional SCON overrides supplied by manager configuration
	SCONRepoOverride       string
	SCONURLLinuxOverride   string
	SCONURLWindowsOverride string
}

// NewSteam constructs a Steam helper bound to a Steam app ID and environment.
func NewSteam(steamID string, logger *utils.Logger, paths *utils.Paths) *Steam {
	return &Steam{
		SteamID: steamID,
		Logger:  logger,
		Paths:   paths,
	}
}

// SetProgressReporter sets a callback that receives progress updates.
func (s *Steam) SetProgressReporter(component string, reporter func(component, stage string, downloaded, total int64)) {
	s.progressComponent = component
	s.progressReporter = reporter
}

// SetSCONOverrides configures optional overrides for SCON downloads and repository.
func (s *Steam) SetSCONOverrides(repo, linuxURL, windowsURL string) {
	s.SCONRepoOverride = strings.TrimSpace(repo)
	s.SCONURLLinuxOverride = strings.TrimSpace(linuxURL)
	s.SCONURLWindowsOverride = strings.TrimSpace(windowsURL)
}

func (s *Steam) reportProgress(stage string, downloaded, total int64) {
	if s.progressReporter == nil || s.progressComponent == "" {
		return
	}
	s.progressReporter(s.progressComponent, stage, downloaded, total)
}

type progressWriter struct {
	writer  io.Writer
	stage   string
	total   int64
	written int64
	report  func(stage string, downloaded, total int64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 && pw.report != nil {
		pw.written += int64(n)
		pw.report(pw.stage, pw.written, pw.total)
	}
	return n, err
}

// SteamAPIResponse models a subset of the steamcmd info API response.
type SteamAPIResponse struct {
	Data map[string]struct {
		Depots struct {
			Branches map[string]struct {
				BuildID string `json:"buildid"`
			} `json:"branches"`
		} `json:"depots"`
	} `json:"data"`
}

// GetVersions returns the current public and beta build IDs for the Steam app.
func (s *Steam) GetVersions() ([]string, error) {
	url := fmt.Sprintf("https://api.steamcmd.net/v1/info/%s", s.SteamID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp SteamAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	data, ok := apiResp.Data[s.SteamID]
	if !ok {
		return nil, fmt.Errorf("version information not found for Steam ID: %s", s.SteamID)
	}

	releaseVersion := data.Depots.Branches["public"].BuildID
	betaVersion := data.Depots.Branches["beta"].BuildID

	if releaseVersion == "" || betaVersion == "" {
		return nil, fmt.Errorf("version information not found")
	}

	s.Logger.Write(fmt.Sprintf("Release version: %s | Beta version: %s", releaseVersion, betaVersion))
	return []string{releaseVersion, betaVersion}, nil
}

// UpdateSteamCMD downloads and installs SteamCMD for the current platform.
func (s *Steam) UpdateSteamCMD() error {
	os.MkdirAll(s.Paths.SteamDir(), os.ModePerm)

	var steamcmdURL, steamcmdFile string
	if runtime.GOOS == "windows" {
		steamcmdURL = STEAMCMD_WIN_URL
		steamcmdFile = "steamcmd.zip"
	} else {
		steamcmdURL = STEAMCMD_LINUX_URL
		steamcmdFile = "steamcmd_linux.tar.gz"
	}

	filePath := filepath.Join(s.Paths.SteamDir(), steamcmdFile)
	if _, _, err := s.downloadFile(steamcmdURL, filePath, "Downloading"); err != nil {
		return err
	}

	s.Logger.Write(fmt.Sprintf("Extracting %s to %s", steamcmdFile, s.Paths.SteamDir()))
	s.reportProgress("Extracting", 0, 0)

	if runtime.GOOS == "windows" {
		if err := s.unzip(filePath, s.Paths.SteamDir()); err != nil {
			s.reportProgress("Extraction failed", 0, 0)
			return err
		}
	} else {
		if err := s.untar(filePath, s.Paths.SteamDir()); err != nil {
			s.reportProgress("Extraction failed", 0, 0)
			return err
		}
	}

	os.Remove(filePath)
	s.reportProgress("Completed", 0, 0)
	s.Logger.Write("SteamCMD deployed successfully")
	return nil
}

// UpdateGame installs or updates the Stationeers dedicated server (beta optional).
func (s *Steam) UpdateGame(beta bool) error {
	var dir string
	if beta {
		dir = s.Paths.BetaDir()
	} else {
		dir = s.Paths.ReleaseDir()
	}
	// Defensive: ensure the chosen install directory is within the configured root path and
	// produce a sanitized absolute path for SteamCMD (+force_install_dir) usage.
	root := filepath.Clean(s.Paths.RootPath)
	cleanDir := filepath.Clean(dir)
	if absDir, err := filepath.Abs(cleanDir); err == nil {
		cleanDir = absDir
	}
	if rel, err := filepath.Rel(root, cleanDir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid install dir escapes root: %s", dir)
	}
	// Prevent control characters/newlines in path passed to SteamCMD
	if strings.ContainsAny(cleanDir, "\n\r\x00") {
		return fmt.Errorf("invalid characters in install dir path")
	}
	s.Logger.Write(fmt.Sprintf("Starting update for Steam ID: %s to %s", s.SteamID, dir))
	s.reportProgress("Preparing SteamCMD", 0, 0)

	// Validate SteamID is numeric to satisfy command construction safety requirements
	if !isAllDigits(strings.TrimSpace(s.SteamID)) {
		return fmt.Errorf("invalid SteamID: %q", s.SteamID)
	}

	branch := "public"
	if beta {
		branch = "beta"
	}

	// Build an allow-listed argument sequence for SteamCMD. Each token is a distinct argv element,
	// avoiding shell expansion. Tokens are validated before execution to mitigate command injection.
	steamCmd := []string{
		"+force_install_dir", cleanDir,
		"+login", "anonymous",
		"+app_update", s.SteamID,
		"-beta", branch,
		"validate",
		"+quit",
	}
	if err := validateSteamArgs(steamCmd); err != nil {
		return err
	}
	// Resolve and validate the steamcmd executable path using strict containment rules.
	execPath, perr := s.safeSteamCmdExec()
	if perr != nil {
		return perr
	}
	// Log command with sanitized path (arguments already validated)
	s.Logger.Write(fmt.Sprintf("Executing command: %s %s", execPath, strings.Join(steamCmd, " ")))
	cmd := exec.Command(execPath, steamCmd...)

	// Stream output to update log if available, otherwise capture
	var captureOutput bool
	if file := s.Logger.File(); file != nil {
		cmd.Stdout = file
		cmd.Stderr = file
	} else {
		captureOutput = true
	}

	s.reportProgress("Running SteamCMD", 0, 0)
	var output []byte
	var err error
	if captureOutput {
		output, err = cmd.CombinedOutput()
	} else {
		err = cmd.Run()
	}
	if err != nil {
		s.reportProgress("SteamCMD failed", 0, 0)
		s.Logger.Write(fmt.Sprintf("SteamCMD error: %v", err))
		if captureOutput {
			s.Logger.Write(fmt.Sprintf("SteamCMD output: %s", string(output)))
		}
		return err
	}
	if captureOutput {
		s.Logger.Write(fmt.Sprintf("SteamCMD output: %s", string(output)))
	}
	s.Logger.Write("rocketstation_DedicatedServer updated successfully")
	s.reportProgress("Completed", 0, 0)
	return nil
}

// UpdateBepInEx downloads and deploys BepInEx, recording its version when available.
func (s *Steam) UpdateBepInEx() error {
	url, archiveName, version, err := s.resolveBepInExDownload()
	if err != nil {
		return err
	}

	destDir := s.Paths.BepInExDir()
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	zipPath := filepath.Join(destDir, "BepInEx.zip")
	s.Logger.Write(fmt.Sprintf("Downloading BepInEx %s (%s) from %s", version, archiveName, url))
	if _, _, err := s.downloadFile(url, zipPath, "Downloading"); err != nil {
		return err
	}

	s.Logger.Write(fmt.Sprintf("Extracting BepInEx to %s", destDir))
	s.reportProgress("Extracting", 0, 0)
	if err := s.unzip(zipPath, destDir); err != nil {
		s.reportProgress("Extraction failed", 0, 0)
		return err
	}

	os.Remove(zipPath)
	s.reportProgress("Completed", 0, 0)
	if version != "" {
		recorded := sanitizeBepInExVersionTag(version)
		if recorded != "" {
			versionFile := filepath.Join(destDir, bepInExVersionFile)
			if writeErr := os.WriteFile(versionFile, []byte(recorded+"\n"), 0o644); writeErr != nil {
				s.Logger.Write(fmt.Sprintf("Warning: unable to record BepInEx version: %v", writeErr))
			}
		}
	}
	if version != "" {
		s.Logger.Write(fmt.Sprintf("BepInEx %s deployed successfully", version))
	} else {
		s.Logger.Write("BepInEx deployed successfully")
	}
	return nil
}

func (s *Steam) resolveBepInExDownload() (string, string, string, error) {
	req, err := http.NewRequest(http.MethodGet, bepInExLatestAPI, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "SDSM-Manager")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("failed to query BepInEx releases: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", "", err
	}

	var suffix string
	switch runtime.GOOS {
	case "windows":
		suffix = "win_x64"
	case "linux":
		suffix = "linux_x64"
	default:
		return "", "", "", fmt.Errorf("unsupported platform for BepInEx deployment: %s", runtime.GOOS)
	}

	for _, asset := range release.Assets {
		nameLower := strings.ToLower(asset.Name)
		if strings.Contains(nameLower, suffix) && strings.HasSuffix(nameLower, ".zip") && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, asset.Name, release.TagName, nil
		}
	}

	return "", "", "", fmt.Errorf("no BepInEx asset found for platform suffix %s", suffix)
}

// UpdateLaunchPad fetches and deploys Stationeers LaunchPad, flattening the root folder if needed.

func (s *Steam) UpdateLaunchPad() error {
	s.Logger.Write("Updating Stationeers LaunchPad")
	url, archiveName, releaseVersion, err := s.resolveLaunchPadDownload()
	if err != nil {
		return err
	}

	zipPath := filepath.Join(s.Paths.RootPath, archiveName)
	if releaseVersion != "" {
		s.Logger.Write(fmt.Sprintf("Downloading Stationeers LaunchPad %s from %s", releaseVersion, url))
	} else {
		s.Logger.Write(fmt.Sprintf("Downloading Stationeers LaunchPad from %s", url))
	}
	if _, _, err := s.downloadFile(url, zipPath, "Downloading"); err != nil {
		return err
	}

	if err := os.RemoveAll(s.Paths.LaunchPadDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Paths.LaunchPadDir(), os.ModePerm); err != nil {
		return err
	}

	s.reportProgress("Extracting", 0, 0)
	if err := s.unzip(zipPath, s.Paths.LaunchPadDir()); err != nil {
		s.reportProgress("Extraction failed", 0, 0)
		return err
	}
	os.Remove(zipPath)

	if err := flattenSingleDirectory(s.Paths.LaunchPadDir()); err != nil {
		s.Logger.Write(fmt.Sprintf("Warning: unable to flatten LaunchPad directory: %v", err))
	}

	if releaseVersion != "" {
		versionPath := filepath.Join(s.Paths.LaunchPadDir(), launchPadVersionFile)
		clean := strings.TrimSpace(releaseVersion)
		if clean != "" {
			if werr := os.WriteFile(versionPath, []byte(clean+"\n"), 0o644); werr != nil {
				s.Logger.Write(fmt.Sprintf("Warning: unable to record LaunchPad version: %v", werr))
			}
		}
	}

	s.reportProgress("Completed", 0, 0)
	if releaseVersion != "" {
		s.Logger.Write(fmt.Sprintf("Stationeers LaunchPad %s deployed successfully", releaseVersion))
	} else {
		s.Logger.Write("Stationeers LaunchPad deployed successfully")
	}
	return nil
}

func (s *Steam) resolveLaunchPadDownload() (string, string, string, error) {
	req, err := http.NewRequest(http.MethodGet, launchPadAPI, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "SDSM-Manager")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.Logger.Write(fmt.Sprintf("Warning: unable to query LaunchPad release API: %v", err))
		return launchPadFallback, "StationeersLaunchPad.zip", "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.Logger.Write(fmt.Sprintf("Warning: LaunchPad release API returned status %d", resp.StatusCode))
		return launchPadFallback, "StationeersLaunchPad.zip", "", nil
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
		ZipballURL string `json:"zipball_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		s.Logger.Write(fmt.Sprintf("Warning: unable to parse LaunchPad release metadata: %v", err))
		return launchPadFallback, "StationeersLaunchPad.zip", "", nil
	}

	version := sanitizeLaunchPadTag(release.TagName)

	for _, asset := range release.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, asset.Name, version, nil
		}
	}

	if release.ZipballURL != "" {
		return release.ZipballURL, "StationeersLaunchPad.zip", version, nil
	}

	return launchPadFallback, "StationeersLaunchPad.zip", "", nil
}

func sanitizeLaunchPadTag(tag string) string {
	value := strings.TrimSpace(tag)
	if value == "" {
		return ""
	}
	if len(value) > 0 {
		switch value[0] {
		case 'v', 'V':
			value = value[1:]
		}
	}
	return strings.TrimSpace(value)
}

// UpdateSCON downloads the latest SCON release (from GitHub) and places its contents into BepInEx/plugins.
// The source repo can be overridden via configuration in the manager (format: owner/repo).
func (s *Steam) UpdateSCON() error {
	repo := strings.TrimSpace(s.SCONRepoOverride)
	if repo == "" {
		repo = sconDefaultRepo
	}
	url, assetName, tag, err := s.resolveSCONDownload(repo)
	if err != nil {
		return err
	}

	// Ensure SCON directory exists
	sconDir := s.Paths.SCONDir()
	if err := os.MkdirAll(sconDir, os.ModePerm); err != nil {
		return err
	}

	// Download to a temporary path outside target dir to avoid deleting the archive during cleanup
	tmpFile, err := os.CreateTemp(s.Paths.RootPath, "SCON-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if tag != "" {
		s.Logger.Write(fmt.Sprintf("Downloading SCON %s (%s) from %s", tag, assetName, url))
	} else if assetName != "" {
		s.Logger.Write(fmt.Sprintf("Downloading SCON (%s) from %s", assetName, url))
	} else {
		s.Logger.Write(fmt.Sprintf("Downloading SCON from %s", url))
	}
	if _, _, err := s.downloadFile(url, tmpPath, "Downloading"); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Clear existing contents before extracting new version
	if err := os.RemoveAll(sconDir); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.MkdirAll(sconDir, os.ModePerm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	s.reportProgress("Extracting", 0, 0)
	if err := s.unzip(tmpPath, sconDir); err != nil {
		s.reportProgress("Extraction failed", 0, 0)
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(tmpPath)

	// If the archive contains a single root folder, flatten it
	if err := flattenSingleDirectory(sconDir); err != nil {
		s.Logger.Write(fmt.Sprintf("Warning: unable to flatten SCON directory: %v", err))
	}

	// Persist the deployed version tag if available so the Manager can report it
	if tag != "" {
		versionPath := filepath.Join(sconDir, sconVersionFile)
		if werr := os.WriteFile(versionPath, []byte(strings.TrimSpace(tag)+"\n"), 0o644); werr != nil {
			s.Logger.Write(fmt.Sprintf("Warning: unable to record SCON version: %v", werr))
		}
	}

	s.reportProgress("Completed", 0, 0)
	if tag != "" {
		s.Logger.Write(fmt.Sprintf("SCON %s deployed into bin/SCON successfully", tag))
	} else {
		s.Logger.Write("SCON deployed into bin/SCON successfully")
	}
	return nil
}

func (s *Steam) resolveSCONDownload(repo string) (string, string, string, error) { // returns url, name, tag
	// Allow explicit URL overrides via configuration for quick pinning/testing
	switch runtime.GOOS {
	case "linux":
		if v := strings.TrimSpace(s.SCONURLLinuxOverride); v != "" {
			return v, filepath.Base(v), "", nil
		}
	case "windows":
		if v := strings.TrimSpace(s.SCONURLWindowsOverride); v != "" {
			return v, filepath.Base(v), "", nil
		}
	default:
		return "", "", "", fmt.Errorf("unsupported platform for SCON deployment: %s", runtime.GOOS)
	}

	// Query GitHub API for latest release assets
	api := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "SDSM-Manager")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Fallback to zipball of main branch
		fallback := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.zip", repo)
		return fallback, "SCON.zip", "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fallback := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.zip", repo)
		return fallback, "SCON.zip", "", nil
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
		ZipballURL string `json:"zipball_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fallback := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.zip", repo)
		return fallback, "SCON.zip", "", nil
	}

	// Prefer OS-specific asset
	osKey := runtime.GOOS // "linux" or "windows"
	var genericZip string
	var genericName string
	for _, asset := range release.Assets {
		nameLower := strings.ToLower(asset.Name)
		if !strings.HasSuffix(nameLower, ".zip") || asset.BrowserDownloadURL == "" {
			continue
		}
		if genericZip == "" {
			genericZip = asset.BrowserDownloadURL
			genericName = asset.Name
		}
		if strings.Contains(nameLower, osKey) {
			return asset.BrowserDownloadURL, asset.Name, release.TagName, nil
		}
	}
	if genericZip != "" {
		return genericZip, genericName, release.TagName, nil
	}
	if release.ZipballURL != "" {
		return release.ZipballURL, "SCON.zip", release.TagName, nil
	}
	fallback := fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.zip", repo)
	return fallback, "SCON.zip", "", nil
}

func (s *Steam) GetSCONLatestTag() (string, error) {
	repo := strings.TrimSpace(s.SCONRepoOverride)
	if repo == "" {
		repo = sconDefaultRepo
	}
	_, _, tag, err := s.resolveSCONDownload(repo)
	if err != nil {
		return "", err
	}
	return tag, nil
}

// unzipSCONToPlugins extracts only relevant plugin files into the plugins directory.
// It handles archives that contain BepInEx/plugins paths or DLLs at root/subfolders.
// unzipSCONToPlugins removed; using generic unzip() into bin/SCON

func flattenSingleDirectory(base string) error {
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}

	root := filepath.Join(base, entries[0].Name())
	inner, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range inner {
		src := filepath.Join(root, entry.Name())
		dst := filepath.Join(base, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	return os.RemoveAll(root)
}

func (s *Steam) downloadFile(url, filepath, stage string) (int64, int64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "SDSM-Manager")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return 0, 0, fmt.Errorf("failed to download %s: %s", url, resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return 0, 0, err
	}
	defer out.Close()

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}

	s.reportProgress(stage, 0, total)
	writer := &progressWriter{
		writer: out,
		stage:  stage,
		total:  total,
		report: func(stage string, downloaded, total int64) {
			s.reportProgress(stage, downloaded, total)
		},
	}

	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		s.reportProgress(stage+" failed", writer.written, total)
		return writer.written, total, err
	}

	s.reportProgress(stage, written, total)
	return written, total, nil
}

func (s *Steam) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			dirPerm := ensureOwnerWritable(f.Mode(), true)
			if err := os.MkdirAll(fpath, dirPerm); err != nil {
				return err
			}
			if err := os.Chmod(fpath, dirPerm); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		filePerm := ensureOwnerWritable(f.Mode(), false)
		if _, statErr := os.Stat(fpath); statErr == nil {
			if err := os.Chmod(fpath, filePerm); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err := os.Chmod(fpath, filePerm); err != nil {
			return err
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Steam) untar(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", target)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			dirPerm := ensureOwnerWritable(os.FileMode(header.Mode), true)
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return err
			}
			if err := os.Chmod(target, dirPerm); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
				return err
			}

			filePerm := ensureOwnerWritable(os.FileMode(header.Mode), false)
			if _, statErr := os.Stat(target); statErr == nil {
				if err := os.Chmod(target, filePerm); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, filePerm)
			if err != nil {
				return err
			}

			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}

			file.Close()
			if err := os.Chmod(target, filePerm); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureOwnerWritable(perm os.FileMode, isDir bool) os.FileMode {
	if perm&0o200 == 0 {
		perm |= 0o200
	}
	if isDir && perm&0o100 == 0 {
		perm |= 0o100
	}
	return perm
}

func (s *Steam) steamCmdExecutable() string {
	if runtime.GOOS == "windows" {
		return "steamcmd.exe"
	}
	return "steamcmd.sh"
}

// safeSteamCmdExec constructs the path to the steamcmd executable under the configured
// root path using SecureJoin and performs additional validation to reduce the risk
// of executing a user-controlled binary:
//   - root path must be absolute
//   - no control characters in root path
//   - steamcmd directory and executable path are contained via SecureJoin
//   - the executable itself must not be a symlink
//   - symlink evaluation of the containing directory must remain within root
//
// If any check fails an error is returned.
func (s *Steam) safeSteamCmdExec() (string, error) {
	root := strings.TrimSpace(s.Paths.RootPath)
	if root == "" {
		return "", fmt.Errorf("empty root path")
	}
	if strings.ContainsAny(root, "\n\r\x00") {
		return "", fmt.Errorf("invalid characters in root path")
	}
	// Ensure absolute
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("unable to resolve absolute root: %v", err)
	}
	// Build steamcmd directory securely
	steamDir, err := utils.SecureJoin(absRoot, filepath.Join("bin", "steamcmd"))
	if err != nil {
		return "", fmt.Errorf("steamcmd directory containment failed: %v", err)
	}
	exeName := s.steamCmdExecutable()
	execPath, err := utils.SecureJoin(steamDir, exeName)
	if err != nil {
		return "", fmt.Errorf("steamcmd executable containment failed: %v", err)
	}
	// Directory symlink evaluation: ensure still inside absRoot after resolving
	evalSteamDir, err := filepath.EvalSymlinks(steamDir)
	if err == nil { // non-fatal if symlink resolution fails; treat original path as authoritative
		// Ensure evaluated path is still within root
		rel, rerr := filepath.Rel(absRoot, evalSteamDir)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("steamcmd directory escapes root after symlink resolution")
		}
	}
	// The executable itself must not be a symlink (prevent substitution attacks)
	fi, statErr := os.Lstat(execPath)
	if statErr != nil {
		return "", fmt.Errorf("steamcmd executable missing: %v", statErr)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("steamcmd executable is a symlink: %s", execPath)
	}
	return execPath, nil
}

func sanitizeBepInExVersionTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	tag = strings.TrimPrefix(tag, "v")
	tag = strings.TrimPrefix(tag, "V")
	for i, r := range tag {
		if (r < '0' || r > '9') && r != '.' {
			tag = tag[:i]
			break
		}
	}
	if strings.Count(tag, ".") != 3 {
		return ""
	}
	for _, part := range strings.Split(tag, ".") {
		if part == "" {
			return ""
		}
	}
	return tag
}

// isAllDigits returns true if s contains only ASCII digits 0-9 and is non-empty.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// validateSteamArgs performs conservative validation of the SteamCMD argv sequence.
// It ensures only expected tokens are present and that values meet basic constraints.
func validateSteamArgs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no arguments provided")
	}
	// Allowed fixed flags/tokens
	allowed := map[string]bool{
		"+force_install_dir": true,
		"+login":             true,
		"+app_update":        true,
		"-beta":              true,
		"beta":               true,
		"public":             true,
		"validate":           true,
		"+quit":              true,
		"anonymous":          true,
	}
	// Walk tokens and ensure ordering constraints for pairs
	for i := 0; i < len(args); i++ {
		tok := strings.TrimSpace(args[i])
		// First-level sanity: disallow NULs or newlines in any token
		if strings.ContainsRune(tok, '\x00') || strings.ContainsRune(tok, '\n') || strings.ContainsRune(tok, '\r') {
			return fmt.Errorf("invalid control characters in argument")
		}
		switch tok {
		case "+force_install_dir":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for +force_install_dir")
			}
			// Accept any dir value; exec.Command passes argv without shell expansion.
			i++
		case "+login":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for +login")
			}
			if strings.TrimSpace(args[i+1]) != "anonymous" {
				return fmt.Errorf("only anonymous login supported")
			}
			i++
		case "+app_update":
			if i+1 >= len(args) {
				return fmt.Errorf("missing SteamID for +app_update")
			}
			if !isAllDigits(strings.TrimSpace(args[i+1])) {
				return fmt.Errorf("invalid SteamID value")
			}
			i++
		case "-beta":
			if i+1 >= len(args) {
				return fmt.Errorf("missing branch after -beta")
			}
			b := strings.TrimSpace(args[i+1])
			if b != "beta" && b != "public" {
				return fmt.Errorf("invalid branch %q", b)
			}
			i++
		default:
			if !allowed[tok] {
				// Token not in allowlist: reject
				return fmt.Errorf("unexpected token %q in SteamCMD args", tok)
			}
		}
	}
	return nil
}
