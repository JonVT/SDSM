package handlers

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"

	"github.com/gin-gonic/gin"
)

func (h *ManagerHandlers) UpdatePOST(c *gin.Context) {
	if !middleware.ValidateFormData(c, []string{}) { // No required fields for this form
		return
	}

	isAsync := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/json") ||
		strings.EqualFold(c.GetHeader("X-Requested-With"), "XMLHttpRequest")

	var deployType manager.DeployType
	var deployErr error
	actionHandled := false

	if c.PostForm("update_config") != "" {
		actionHandled = true
		steamID := middleware.SanitizeString(c.PostForm("steam_id"))
		rootPath := middleware.SanitizeFilename(c.PostForm("root_path"))
		portStr := c.PostForm("port")
		language := middleware.SanitizeString(c.PostForm("language"))

		port, err := middleware.ValidatePort(portStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "manager.html", gin.H{
				"error": "Invalid port number",
			})
			return
		}

		t, _ := time.Parse("15:04:05", c.PostForm("auto_update"))
		startupUpdate := c.PostForm("start_update") == "on"

		h.manager.UpdateConfig(steamID, rootPath, port, t, startupUpdate)
		if language != "" {
			h.manager.Language = language
			h.manager.Save()
		}
	} else if c.PostForm("update_release") != "" {
		actionHandled = true
		deployType = manager.DeployTypeRelease
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_beta") != "" {
		actionHandled = true
		deployType = manager.DeployTypeBeta
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_steamcmd") != "" {
		actionHandled = true
		deployType = manager.DeployTypeSteamCMD
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_bepinex") != "" {
		actionHandled = true
		deployType = manager.DeployTypeBepInEx
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_launchpad") != "" {
		actionHandled = true
		deployType = manager.DeployTypeLaunchPad
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_all") != "" {
		actionHandled = true
		deployType = manager.DeployTypeAll
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("shutdown") != "" {
		actionHandled = true
		go h.manager.Shutdown()
	} else if c.PostForm("restart") != "" {
		actionHandled = true
		go h.manager.Restart()
	}

	if isAsync {
		if deployType != "" {
			if deployErr != nil {
				c.JSON(http.StatusConflict, gin.H{
					"error": deployErr.Error(),
				})
				return
			}
			c.JSON(http.StatusAccepted, gin.H{
				"status":      "started",
				"deploy_type": deployType,
			})
			return
		}

		if actionHandled {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "no action specified"})
		return
	}

	c.Redirect(http.StatusFound, "/manager")
}

func (h *ManagerHandlers) UpdateProgressGET(c *gin.Context) {
	c.JSON(http.StatusOK, h.manager.ProgressSnapshot())
}

func (h *ManagerHandlers) UpdateStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flush := func() {
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}

	sendSnapshot := func() {
		c.SSEvent("progress", h.manager.ProgressSnapshot())
		flush()
	}

	sendSnapshot()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendSnapshot()
		}
	}
}

func (h *ManagerHandlers) UpdateLogGET(c *gin.Context) {
	logPath := h.manager.Paths.UpdateLogFile()
	if logPath == "" {
		c.String(http.StatusNotFound, "Update log is not available.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.String(http.StatusOK, "Update log is empty.")
			return
		}
		c.String(http.StatusInternalServerError, "Unable to open update log.")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Unable to read update log.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func (h *ManagerHandlers) ManagerLogGET(c *gin.Context) {
	logPath := h.manager.Paths.LogFile()
	if logPath == "" {
		c.String(http.StatusNotFound, "Application log is not available.")
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.String(http.StatusOK, "Application log is empty.")
			return
		}
		c.String(http.StatusInternalServerError, "Unable to open application log.")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Unable to read application log.")
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}
