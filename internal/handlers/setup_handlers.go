package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"sdsm/internal/manager"

	"github.com/gin-gonic/gin"
)

func (h *ManagerHandlers) SetupGET(c *gin.Context) {
	if !h.manager.IsUpdating() {
		h.manager.CheckMissingComponents()
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{
		"missingComponents": h.manager.GetMissingComponents(),
		"paths":             h.manager.Paths,
		"deployErrors":      h.manager.GetDeployErrors(),
	})
}

func (h *ManagerHandlers) SetupSkipPOST(c *gin.Context) {
	h.manager.NeedsUploadPrompt = false
	h.manager.Log.Write("User skipped initial setup")

	if strings.Contains(c.GetHeader("Accept"), "application/json") {
		c.JSON(http.StatusOK, gin.H{
			"status":  "skipped",
			"message": "Setup skipped",
		})
		return
	}

	c.Redirect(http.StatusFound, "/manager")
}

func (h *ManagerHandlers) SetupInstallPOST(c *gin.Context) {
	if h.manager.SetupInProgress {
		c.JSON(http.StatusConflict, gin.H{
			"status":  "error",
			"message": "Setup is already in progress",
		})
		return
	}

	missing := h.manager.GetMissingComponents()
	if len(missing) == 0 {
		h.manager.CheckMissingComponents()
		missing = h.manager.GetMissingComponents()
	}

	deployList, fallbackAll := determineSetupDeployTargets(missing, h.manager.ServerCount())
	if !fallbackAll && len(deployList) == 0 {
		message := "No missing components detected"
		h.manager.Log.Write("Setup requested but no missing components detected; skipping automatic install")
		c.JSON(http.StatusOK, gin.H{
			"status":  "noop",
			"message": message,
		})
		return
	}

	h.manager.SetupInProgress = true
	h.manager.Log.Write("User initiated automatic setup for missing components")

	go func(missing []string, deployList []manager.DeployType, fallbackAll bool) {
		defer func() {
			h.manager.SetupInProgress = false
		}()

		if fallbackAll || len(deployList) == 0 {
			h.manager.Log.Write("Automatic setup will deploy all components")
			if err := h.manager.Deploy(manager.DeployTypeAll); err != nil {
				h.manager.Log.Write(fmt.Sprintf("Automatic setup failed: %v", err))
				return
			}
			h.manager.CheckMissingComponents()
			if remaining := h.manager.GetMissingComponents(); len(remaining) > 0 {
				h.manager.Log.Write(fmt.Sprintf("Components still missing after setup: %v", remaining))
				return
			}
			h.manager.Log.Write("Automatic setup completed successfully")
			return
		}

		componentOrder := make([]string, len(deployList))
		for i, dt := range deployList {
			componentOrder[i] = string(dt)
		}
		h.manager.Log.Write(fmt.Sprintf("Automatic setup will deploy components: %s", strings.Join(componentOrder, ", ")))

		allSucceeded := true
		for _, dt := range deployList {
			if err := h.manager.Deploy(dt); err != nil {
				h.manager.Log.Write(fmt.Sprintf("Automatic setup failed while deploying %s: %v", dt, err))
				allSucceeded = false
			}
		}

		h.manager.CheckMissingComponents()
		if remaining := h.manager.GetMissingComponents(); len(remaining) > 0 {
			h.manager.Log.Write(fmt.Sprintf("Components still missing after setup: %v", remaining))
			allSucceeded = false
		}

		if allSucceeded {
			h.manager.Log.Write("Automatic setup completed successfully")
		}
	}(append([]string(nil), missing...), append([]manager.DeployType(nil), deployList...), fallbackAll)

	message := "Setup started in background"
	if fallbackAll {
		message = "Setup started for all components"
	} else if len(missing) > 0 {
		message = fmt.Sprintf("Setup started for: %s", strings.Join(missing, ", "))
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "started",
		"message": message,
	})
}

func (h *ManagerHandlers) SetupStatusGET(c *gin.Context) {
	updating := h.manager.IsUpdating()
	if !updating {
		h.manager.CheckMissingComponents()
	}
	c.JSON(http.StatusOK, gin.H{
		"inProgress":        h.manager.SetupInProgress,
		"needsUploadPrompt": h.manager.NeedsUploadPrompt,
		"updating":          updating,
		"errors":            h.manager.GetDeployErrors(),
		"missingComponents": h.manager.GetMissingComponents(),
		"lastUpdateLog":     h.manager.LastUpdateLogLine(),
	})
}

func (h *ManagerHandlers) SetupUpdatePOST(c *gin.Context) {
	h.manager.NeedsUploadPrompt = false
	h.manager.Log.Write("User requested auto-update for missing components")

	if err := h.startDeployAsync(manager.DeployTypeAll); err != nil {
		h.manager.Log.Write(fmt.Sprintf("Unable to start setup update: %v", err))
		c.Redirect(http.StatusFound, "/login?message=Unable to start update. Another deployment may already be running.")
		return
	}

	c.Redirect(http.StatusFound, "/login?message=Update started. Please wait for components to download.")
}

func determineSetupDeployTargets(missing []string, serverCount int) ([]manager.DeployType, bool) {
	if len(missing) == 0 {
		return nil, false
	}

	required := make(map[manager.DeployType]bool)
	needsServerRedeploy := false
	fallbackAll := false

	for _, component := range missing {
		switch component {
		case "SteamCMD":
			required[manager.DeployTypeSteamCMD] = true
		case "Stationeers Release":
			required[manager.DeployTypeRelease] = true
			needsServerRedeploy = true
		case "Stationeers LaunchPad":
			required[manager.DeployTypeLaunchPad] = true
			needsServerRedeploy = true
		case "BepInEx":
			required[manager.DeployTypeBepInEx] = true
			needsServerRedeploy = true
		default:
			fallbackAll = true
		}
	}

	if fallbackAll {
		return nil, true
	}

	if needsServerRedeploy && serverCount > 0 {
		required[manager.DeployTypeServers] = true
	}

	ordered := []manager.DeployType{
		manager.DeployTypeSteamCMD,
		manager.DeployTypeRelease,
		manager.DeployTypeBeta,
		manager.DeployTypeBepInEx,
		manager.DeployTypeLaunchPad,
		manager.DeployTypeServers,
	}

	var result []manager.DeployType
	for _, dt := range ordered {
		if required[dt] {
			result = append(result, dt)
		}
	}

	return result, false
}
