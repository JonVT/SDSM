package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxPathEntries = 750

type pathEntry struct {
	Name          string `json:"name"`
	RelPath       string `json:"relPath"`
	FullPath      string `json:"fullPath"`
	DisplayPath   string `json:"displayPath"`
	IsDir         bool   `json:"isDir"`
	Selectable    bool   `json:"selectable"`
	Size          int64  `json:"size,omitempty"`
	ModifiedAt    string `json:"modifiedAt,omitempty"`
	IsSymlink     bool   `json:"isSymlink,omitempty"`
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
}

type pathBreadcrumb struct {
	Label   string `json:"label"`
	RelPath string `json:"relPath"`
}

type pathBrowseResponse struct {
	RootPath         string           `json:"rootPath"`
	RootName         string           `json:"rootName"`
	CurrentPath      string           `json:"currentPath"`
	CurrentRelPath   string           `json:"currentRelPath"`
	ParentRelPath    string           `json:"parentRelPath,omitempty"`
	Mode             string           `json:"mode"`
	CanSelectCurrent bool             `json:"canSelectCurrent"`
	Breadcrumbs      []pathBreadcrumb `json:"breadcrumbs"`
	Entries          []pathEntry      `json:"entries"`
	LimitHit         bool             `json:"limitHit"`
	PreselectRelPath string           `json:"preselectRelPath,omitempty"`
	PreselectFull    string           `json:"preselectFullPath,omitempty"`
	PreselectName    string           `json:"preselectName,omitempty"`
	PreselectIsDir   bool             `json:"preselectIsDir,omitempty"`
}

// APIPathBrowser lists directories/files under the configured root path for use by the UI path picker widget.
// Query params:
//
//	path: relative (default "")
//	mode: directory | file | any (default directory)
func (h *ManagerHandlers) APIPathBrowser(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	rootPath := ""
	if h.manager != nil && h.manager.Paths != nil {
		rootPath = strings.TrimSpace(h.manager.Paths.RootPath)
	}
	if rootPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "root path not configured"})
		return
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to resolve root path"})
		return
	}
	rootAbs = filepath.Clean(rootAbs)

	mode := normalizePathPickerMode(c.DefaultQuery("mode", "directory"))

	requested := strings.TrimSpace(c.Query("path"))
	var targetDir string = rootAbs
	var preselectAbs string
	if requested != "" {
		candidate := requested
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(rootAbs, candidate)
		}
		candidate = filepath.Clean(candidate)
		if !isWithinRoot(rootAbs, candidate) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path must be inside the configured root path"})
			return
		}
		info, statErr := os.Stat(candidate)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				c.JSON(http.StatusNotFound, gin.H{"error": "path does not exist"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to inspect path"})
			return
		}
		if info.IsDir() {
			targetDir = candidate
		} else {
			targetDir = filepath.Dir(candidate)
			preselectAbs = candidate
		}
	}

	dirInfo, err := os.Stat(targetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "directory does not exist"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to read directory"})
		return
	}
	if !dirInfo.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target is not a directory"})
		return
	}
	listing, err := os.ReadDir(targetDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to list directory"})
		return
	}

	entries := make([]pathEntry, 0, len(listing))
	limitHit := false
	for _, entry := range listing {
		name := entry.Name()
		full := filepath.Join(targetDir, name)
		full = filepath.Clean(full)
		if !isWithinRoot(rootAbs, full) {
			continue
		}
		rel, relErr := filepath.Rel(rootAbs, full)
		if relErr != nil {
			continue
		}
		rel = normalizeRelPath(rel)
		info, infoErr := entry.Info()
		isSymlink := entry.Type()&os.ModeSymlink != 0
		isDir := entry.IsDir()
		if infoErr == nil {
			isDir = info.IsDir()
		}
		var size int64
		var modified string
		if infoErr == nil {
			size = info.Size()
			modified = info.ModTime().Format(time.RFC3339)
		}
		selectable := false
		switch mode {
		case "file":
			selectable = !isDir
		case "any":
			selectable = true
		default:
			selectable = isDir
		}
		entryData := pathEntry{
			Name:        name,
			RelPath:     rel,
			FullPath:    full,
			DisplayPath: filepath.ToSlash(full),
			IsDir:       isDir,
			Selectable:  selectable,
			Size:        size,
			ModifiedAt:  modified,
			IsSymlink:   isSymlink,
		}
		if isSymlink {
			if linkTarget, err := os.Readlink(full); err == nil {
				entryData.SymlinkTarget = linkTarget
			}
		}
		entries = append(entries, entryData)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir && !entries[j].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	if len(entries) > maxPathEntries {
		entries = entries[:maxPathEntries]
		limitHit = true
	}

	currentRel := normalizeRelPath(mustRel(rootAbs, targetDir))
	parentRel := parentRelPath(currentRel)
	preselectRel := normalizeRelPath(mustRel(rootAbs, preselectAbs))

	rootName := filepath.Base(rootAbs)
	if rootName == "." || rootName == "" || rootName == string(filepath.Separator) {
		rootName = "Root"
	}

	preselectName := ""
	if preselectAbs != "" {
		name := filepath.Base(preselectAbs)
		if name == "." || name == "" {
			name = filepath.ToSlash(preselectAbs)
		}
		preselectName = name
	}

	resp := pathBrowseResponse{
		RootPath:         filepath.ToSlash(rootAbs),
		RootName:         rootName,
		CurrentPath:      filepath.ToSlash(targetDir),
		CurrentRelPath:   currentRel,
		ParentRelPath:    parentRel,
		Mode:             mode,
		CanSelectCurrent: mode != "file",
		Breadcrumbs:      buildBreadcrumbs(rootAbs, targetDir),
		Entries:          entries,
		LimitHit:         limitHit,
		PreselectRelPath: preselectRel,
		PreselectFull:    filepath.ToSlash(preselectAbs),
		PreselectName:    preselectName,
	}
	if preselectAbs != "" {
		info, err := os.Stat(preselectAbs)
		if err == nil {
			resp.PreselectIsDir = info.IsDir()
		}
	}

	c.JSON(http.StatusOK, resp)
}

func normalizePathPickerMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "file", "files", "path", "paths":
		return "file"
	case "any", "all", "both":
		return "any"
	default:
		return "directory"
	}
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func normalizeRelPath(rel string) string {
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func parentRelPath(rel string) string {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return ""
	}
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func mustRel(root, target string) string {
	if strings.TrimSpace(target) == "" {
		return ""
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return ""
	}
	return rel
}

func buildBreadcrumbs(root, target string) []pathBreadcrumb {
	rootLabel := filepath.Base(root)
	if rootLabel == "." || rootLabel == "" || rootLabel == string(filepath.Separator) {
		rootLabel = "Root"
	}
	crumbs := []pathBreadcrumb{{Label: rootLabel, RelPath: ""}}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return crumbs
	}
	rel = normalizeRelPath(rel)
	if rel == "" {
		return crumbs
	}
	parts := strings.Split(rel, "/")
	var accum string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if accum == "" {
			accum = part
		} else {
			accum = accum + "/" + part
		}
		crumbs = append(crumbs, pathBreadcrumb{Label: part, RelPath: accum})
	}
	return crumbs
}
