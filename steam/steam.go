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
	STEAMCMD_WIN_URL   = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip"
	STEAMCMD_LINUX_URL = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"
	bepInExLatestAPI   = "https://api.github.com/repos/BepInEx/BepInEx/releases/latest"
	launchPadAPI       = "https://api.github.com/repos/StationeersLaunchPad/StationeersLaunchPad/releases/latest"
	launchPadFallback  = "https://github.com/StationeersLaunchPad/StationeersLaunchPad/archive/refs/heads/main.zip"
	bepInExVersionFile = "bepinex.version"
)

type Steam struct {
	SteamID           string
	Logger            *utils.Logger
	Paths             *utils.Paths
	progressReporter  func(component, stage string, downloaded, total int64)
	progressComponent string
}

func NewSteam(steamID string, logger *utils.Logger, paths *utils.Paths) *Steam {
	return &Steam{
		SteamID: steamID,
		Logger:  logger,
		Paths:   paths,
	}
}

func (s *Steam) SetProgressReporter(component string, reporter func(component, stage string, downloaded, total int64)) {
	s.progressComponent = component
	s.progressReporter = reporter
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

type SteamAPIResponse struct {
	Data map[string]struct {
		Depots struct {
			Branches map[string]struct {
				BuildID string `json:"buildid"`
			} `json:"branches"`
		} `json:"depots"`
	} `json:"data"`
}

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

func (s *Steam) UpdateGame(beta bool) error {
	var dir string
	if beta {
		dir = s.Paths.BetaDir()
	} else {
		dir = s.Paths.ReleaseDir()
	}
	s.Logger.Write(fmt.Sprintf("Starting update for Steam ID: %s to %s", s.SteamID, dir))
	s.reportProgress("Preparing SteamCMD", 0, 0)

	steamCmd := []string{
		"+force_install_dir", dir,
		"+login", "anonymous",
		"+app_update", s.SteamID,
	}
	if beta {
		steamCmd = append(steamCmd, "-beta", "beta")
	}
	steamCmd = append(steamCmd, "validate", "+quit")

	steamCmdPath := filepath.Join(s.Paths.SteamDir(), s.steamCmdExecutable())
	s.Logger.Write(fmt.Sprintf("Executing command: %s %s", steamCmdPath, strings.Join(steamCmd, " ")))
	cmd := exec.Command(steamCmdPath, steamCmd...)

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

func (s *Steam) UpdateLaunchPad() error {
	s.Logger.Write("Updating Stationeers LaunchPad")
	url, archiveName, err := s.resolveLaunchPadDownload()
	if err != nil {
		return err
	}

	zipPath := filepath.Join(s.Paths.RootPath, archiveName)
	s.Logger.Write(fmt.Sprintf("Downloading Stationeers LaunchPad from %s", url))
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

	s.reportProgress("Completed", 0, 0)
	s.Logger.Write("Stationeers LaunchPad deployed successfully")
	return nil
}

func (s *Steam) resolveLaunchPadDownload() (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, launchPadAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "SDSM-Manager")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.Logger.Write(fmt.Sprintf("Warning: unable to query LaunchPad release API: %v", err))
		return launchPadFallback, "StationeersLaunchPad.zip", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.Logger.Write(fmt.Sprintf("Warning: LaunchPad release API returned status %d", resp.StatusCode))
		return launchPadFallback, "StationeersLaunchPad.zip", nil
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
		return launchPadFallback, "StationeersLaunchPad.zip", nil
	}

	for _, asset := range release.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, asset.Name, nil
		}
	}

	if release.ZipballURL != "" {
		return release.ZipballURL, "StationeersLaunchPad.zip", nil
	}

	return launchPadFallback, "StationeersLaunchPad.zip", nil
}

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
