package handlers

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"sdsm/app/backend/internal/constants"
	"sdsm/app/backend/internal/integrations/discord"
	"sdsm/app/backend/internal/version"
)


type bugReportRequest struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	IncludeManagerLog  bool   `json:"include_manager_log"`
	IncludeUpdateLog   bool   `json:"include_update_log"`
	IncludeEnvironment bool   `json:"include_environment"`
}

func (h *ManagerHandlers) BugReportPOST(c *gin.Context) {
	if h == nil || h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "uninitialized"})
		return
	}
	// Use fixed webhook constant from shared constants package
	wh := constants.SDSMCommunityBugReportWebhook
	var req bugReportRequest
	if strings.Contains(c.GetHeader("Content-Type"), "application/json") {
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}
	} else {
		req.Title = strings.TrimSpace(c.PostForm("title"))
		req.Description = strings.TrimSpace(c.PostForm("description"))
		req.IncludeManagerLog = c.PostForm("include_manager_log") == "1" || strings.EqualFold(c.PostForm("include_manager_log"), "true")
		req.IncludeUpdateLog = c.PostForm("include_update_log") == "1" || strings.EqualFold(c.PostForm("include_update_log"), "true")
		req.IncludeEnvironment = c.PostForm("include_environment") == "1" || strings.EqualFold(c.PostForm("include_environment"), "true")
	}
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title required"})
		return
	}

	desc := strings.TrimSpace(req.Description)
	var b strings.Builder
	if desc != "" {
		b.WriteString(desc)
		b.WriteString("\n\n")
	}
	// Environment info
	if req.IncludeEnvironment {
		b.WriteString("Environment\n")
		b.WriteString("```\n")
		b.WriteString(fmt.Sprintf("SDSM: %s\n", version.String()))
		b.WriteString(fmt.Sprintf("Go: %s\n", runtime.Version()))
		b.WriteString(fmt.Sprintf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH))
		b.WriteString("```\n\n")
	}
	// For MVP, inline simple read using utils helper-like code
	readTail := func(path string, max int) string {
		if strings.TrimSpace(path) == "" {
			return ""
		}
		bs, err := utilsTail(path, max)
		if err != nil {
			return ""
		}
		return string(bs)
	}
	if req.IncludeManagerLog {
		if p := h.manager.Paths.LogFile(); p != "" {
			if t := readTail(p, 4000); t != "" {
				b.WriteString("Manager Log (tail)\n```\n")
				b.WriteString(t)
				b.WriteString("\n```\n\n")
			}
		}
	}
	if req.IncludeUpdateLog {
		if p := h.manager.Paths.UpdateLogFile(); p != "" {
			if t := readTail(p, 4000); t != "" {
				b.WriteString("Update Log (tail)\n```\n")
				b.WriteString(t)
				b.WriteString("\n```\n\n")
			}
		}
	}

	embed := discord.NewEmbed(req.Title, truncateForDiscord(b.String(), 3900), 0xF97316, "SDSM bug report")
	payload := discord.WebhookPayload{Embeds: []discord.Embed{embed}}
	if status, err := discord.Post(wh, payload); err != nil || status < 200 || status >= 300 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to deliver to Discord"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func truncateForDiscord(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

// utilsTail is a small local helper to read tail of a file without importing new packages.
// Not optimized for huge files; sufficient for small tails.
func utilsTail(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	start := int64(0)
	if size > int64(max) {
		start = size - int64(max)
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return nil, err
		}
	}
	buf := make([]byte, int(size-start))
	_, _ = f.Read(buf)
	return buf, nil
}
