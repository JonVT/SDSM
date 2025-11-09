package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	actionName := ""

	if c.PostForm("update_config") != "" {
		actionHandled = true
		actionName = "update_config"
		steamID := middleware.SanitizeString(c.PostForm("steam_id"))
		rootPath := middleware.SanitizePath(c.PostForm("root_path"))
		portStr := c.PostForm("port")
		language := middleware.SanitizeString(c.PostForm("language"))
		detachedServers := c.PostForm("detached_servers") == "on"
		trayEnabled := c.PostForm("tray_enabled") == "on"
		tlsEnabled := c.PostForm("tls_enabled") == "on"
		tlsCert := middleware.SanitizePath(strings.TrimSpace(c.PostForm("tls_cert")))
		tlsKey := middleware.SanitizePath(strings.TrimSpace(c.PostForm("tls_key")))

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
		h.manager.DetachedServers = detachedServers
		// Only meaningful on Windows; allow user to toggle off to disable tray next start.
		h.manager.TrayEnabled = trayEnabled
		// TLS is cross-platform; effective after restart
		h.manager.TLSEnabled = tlsEnabled
		h.manager.TLSCertPath = tlsCert
		h.manager.TLSKeyPath = tlsKey
		h.manager.Save()
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
	} else if c.PostForm("update_scon") != "" {
		actionHandled = true
		deployType = manager.DeployTypeSCON
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("update_all") != "" {
		actionHandled = true
		deployType = manager.DeployTypeAll
		deployErr = h.startDeployAsync(deployType)
	} else if c.PostForm("shutdown") != "" {
		actionHandled = true
		actionName = "shutdown"
		go h.manager.Shutdown()
	} else if c.PostForm("restart") != "" {
		actionHandled = true
		actionName = "restart"
		go h.manager.Restart()
	} else if c.PostForm("generate_tls_self_signed") != "" {
		actionHandled = true
		actionName = "generate_tls_self_signed"
		certDir := filepath.Join(h.manager.Paths.RootPath, "certs")
		_ = os.MkdirAll(certDir, 0o755)
		certPath := filepath.Join(certDir, "sdsm.crt")
		keyPath := filepath.Join(certDir, "sdsm.key")
		if err := generateSelfSignedCert(certPath, keyPath); err != nil {
			if isAsync {
				ToastError(c, "TLS Generation Failed", err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.HTML(http.StatusInternalServerError, "manager.html", gin.H{"error": err.Error()})
			return
		}
		h.manager.TLSCertPath = certPath
		h.manager.TLSKeyPath = keyPath
		h.manager.TLSEnabled = true
		h.manager.Save()
		if isAsync {
			ToastSuccess(c, "TLS Ready", "Self-signed certificate generated.")
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
		// Non-async: redirect back to manager with a flag for inline notice
		c.Redirect(http.StatusFound, "/manager?tls=generated")
		return
	}

	if isAsync {
		if deployType != "" {
			if deployErr != nil {
				ToastError(c, "Update Failed", deployErr.Error())
				c.JSON(http.StatusConflict, gin.H{
					"error": deployErr.Error(),
				})
				return
			}
			ToastSuccess(c, "Update Started", string(deployType)+" update started.")
			c.JSON(http.StatusAccepted, gin.H{
				"status":      "started",
				"deploy_type": deployType,
			})
			return
		}

		if actionHandled {
			// Specific toasts for non-deploy actions
			switch actionName {
			case "update_config":
				ToastSuccess(c, "Configuration Saved", "Settings updated.")
			case "shutdown":
				ToastWarn(c, "Shutdown Initiated", "SDSM is shutting down...")
			case "restart":
				ToastInfo(c, "Restarting", "SDSM is restarting...")
			default:
				ToastSuccess(c, "Action Queued", "Your request has been processed.")
			}

			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}

		ToastError(c, "Invalid Request", "No action specified.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "no action specified"})
		return
	}

	c.Redirect(http.StatusFound, "/manager")
}

// generateSelfSignedCert creates a minimal self-signed certificate and key suitable for local HTTPS.
func generateSelfSignedCert(certPath, keyPath string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{Organization: []string{"SDSM"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	// Add localhost SANs
	if ip := net.ParseIP("127.0.0.1"); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return err
	}

	return nil
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
