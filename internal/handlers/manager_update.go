package handlers

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
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
		// Discord webhooks
		dcManager := strings.TrimSpace(c.PostForm("discord_manager_webhook"))
		dcDefault := strings.TrimSpace(c.PostForm("discord_default_webhook"))
		// Notification prefs (booleans)
		notifyEnableDeploy := c.PostForm("notify_enable_deploy") == "on"
		notifyEnableServer := c.PostForm("notify_enable_server") == "on"
		notifyDeployOnStarted := c.PostForm("notify_deploy_on_started") == "on"
		notifyDeployOnCompleted := c.PostForm("notify_deploy_on_completed") == "on"
		notifyDeployOnCompletedError := c.PostForm("notify_deploy_on_completed_error") == "on"
		notifyOnStart := c.PostForm("notify_on_start") == "on"
		notifyOnStopping := c.PostForm("notify_on_stopping") == "on"
		notifyOnStopped := c.PostForm("notify_on_stopped") == "on"
		notifyOnRestart := c.PostForm("notify_on_restart") == "on"
		notifyOnUpdateStarted := c.PostForm("notify_on_update_started") == "on"
		notifyOnUpdateCompleted := c.PostForm("notify_on_update_completed") == "on"
		notifyOnUpdateFailed := c.PostForm("notify_on_update_failed") == "on"
		notifyDeployRelease := c.PostForm("notify_deploy_release") == "on"
		notifyDeployBeta := c.PostForm("notify_deploy_beta") == "on"
		notifyDeployBepInEx := c.PostForm("notify_deploy_bepinex") == "on"
		notifyDeployLaunchPad := c.PostForm("notify_deploy_launchpad") == "on"
		notifyDeploySCON := c.PostForm("notify_deploy_scon") == "on"
		notifyDeploySteamCMD := c.PostForm("notify_deploy_steamcmd") == "on"
		notifyDeployServers := c.PostForm("notify_deploy_servers") == "on"
		// Message templates & colors (accept raw; validated minimally at load/use)
		notifyMsgStart := strings.TrimSpace(c.PostForm("notify_msg_start"))
		notifyMsgStopping := strings.TrimSpace(c.PostForm("notify_msg_stopping"))
		notifyMsgStopped := strings.TrimSpace(c.PostForm("notify_msg_stopped"))
		notifyMsgRestart := strings.TrimSpace(c.PostForm("notify_msg_restart"))
		notifyMsgUpdateStarted := strings.TrimSpace(c.PostForm("notify_msg_update_started"))
		notifyMsgUpdateCompleted := strings.TrimSpace(c.PostForm("notify_msg_update_completed"))
		notifyMsgUpdateFailed := strings.TrimSpace(c.PostForm("notify_msg_update_failed"))
		notifyColorStart := strings.TrimSpace(c.PostForm("notify_color_start"))
		notifyColorStopping := strings.TrimSpace(c.PostForm("notify_color_stopping"))
		notifyColorStopped := strings.TrimSpace(c.PostForm("notify_color_stopped"))
		notifyColorRestart := strings.TrimSpace(c.PostForm("notify_color_restart"))
		notifyColorUpdateStarted := strings.TrimSpace(c.PostForm("notify_color_update_started"))
		notifyColorUpdateCompleted := strings.TrimSpace(c.PostForm("notify_color_update_completed"))
		notifyColorUpdateFailed := strings.TrimSpace(c.PostForm("notify_color_update_failed"))
		// Deploy templates/colors
		notifyMsgDeployStarted := strings.TrimSpace(c.PostForm("notify_msg_deploy_started"))
		notifyMsgDeployCompleted := strings.TrimSpace(c.PostForm("notify_msg_deploy_completed"))
		notifyMsgDeployCompletedError := strings.TrimSpace(c.PostForm("notify_msg_deploy_completed_error"))
		notifyColorDeployStarted := strings.TrimSpace(c.PostForm("notify_color_deploy_started"))
		notifyColorDeployCompleted := strings.TrimSpace(c.PostForm("notify_color_deploy_completed"))
		notifyColorDeployCompletedError := strings.TrimSpace(c.PostForm("notify_color_deploy_completed_error"))
		detachedServers := c.PostForm("detached_servers") == "on"
		trayEnabled := c.PostForm("tray_enabled") == "on"
		tlsEnabled := c.PostForm("tls_enabled") == "on"
		autoPFMgr := c.PostForm("auto_port_forward_manager") == "on"
		tlsCert := middleware.SanitizePath(strings.TrimSpace(c.PostForm("tls_cert")))
		tlsKey := middleware.SanitizePath(strings.TrimSpace(c.PostForm("tls_key")))

		port, err := middleware.ValidatePort(portStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "manager.html", gin.H{
				"error": "Invalid port number",
			})
			return
		}

		autoUpdateInput := strings.TrimSpace(c.PostForm("auto_update"))
		updateTime := time.Time{}
		if autoUpdateInput != "" {
			parsed := false
			for _, layout := range []string{"15:04", "15:04:05"} {
				if candidate, err := time.Parse(layout, autoUpdateInput); err == nil {
					updateTime = candidate
					parsed = true
					break
				}
			}
			if !parsed {
				updateTime = h.manager.UpdateTime
			}
		}
		startupUpdate := c.PostForm("start_update") == "on"

		h.manager.UpdateConfig(steamID, rootPath, port, updateTime, startupUpdate)
		h.manager.DiscordManagerWebhook = dcManager
		h.manager.DiscordDefaultWebhook = dcDefault
		h.manager.DetachedServers = detachedServers
		// Only meaningful on Windows; allow user to toggle off to disable tray next start.
		h.manager.TrayEnabled = trayEnabled
		// TLS is cross-platform; effective after restart
		// Treat provided TLS paths as sources ONLY when absolute; copy into root/certs and persist relative paths.
		h.manager.TLSEnabled = tlsEnabled
		// Manager port forwarding toggle (applies immediately best-effort)
		prevPF := h.manager.AutoPortForwardManager
		h.manager.AutoPortForwardManager = autoPFMgr
		if tlsEnabled {
			certSrc := tlsCert
			keySrc := tlsKey
			if certSrc != "" && !filepath.IsAbs(certSrc) {
				certSrc = ""
			}
			if keySrc != "" && !filepath.IsAbs(keySrc) {
				keySrc = ""
			}
			certRel, keyRel, copyErr := h.manager.InstallTLSFromSources(certSrc, keySrc)
			if copyErr != nil {
				// Disable TLS on failure to avoid broken startup and surface error
				h.manager.TLSEnabled = false
				h.manager.Log.Write("TLS asset installation failed: " + copyErr.Error())
				if isAsync {
					ToastError(c, "TLS Setup Failed", copyErr.Error())
				}
			} else {
				if strings.TrimSpace(certRel) != "" {
					h.manager.TLSCertPath = certRel
				}
				if strings.TrimSpace(keyRel) != "" {
					h.manager.TLSKeyPath = keyRel
				}
				// Validate existing/managed assets before leaving TLS enabled
				if h.manager.TLSEnabled {
					// Resolve paths: prefer relative under root/certs
					certPath := strings.TrimSpace(h.manager.TLSCertPath)
					keyPath := strings.TrimSpace(h.manager.TLSKeyPath)
					if certPath != "" && !filepath.IsAbs(certPath) {
						certPath = filepath.Join(h.manager.Paths.RootPath, certPath)
					}
					if keyPath != "" && !filepath.IsAbs(keyPath) {
						keyPath = filepath.Join(h.manager.Paths.RootPath, keyPath)
					}
					// Read and parse PEM
					if certPath == "" || keyPath == "" {
						h.manager.TLSEnabled = false
						if isAsync {
							ToastError(c, "TLS Invalid", "Certificate or key path missing.")
						}
					} else {
						cerr := validatePEMCertificate(certPath)
						kobj, kerr := validatePEMKey(keyPath)
						if cerr != nil || kerr != nil || !keysMatch(certPath, kobj) {
							h.manager.TLSEnabled = false
							msg := ""
							if cerr != nil {
								msg = cerr.Error()
							}
							if kerr != nil {
								if msg != "" {
									msg += "; "
								}
								msg += kerr.Error()
							}
							if msg == "" {
								msg = "certificate and key do not match"
							}
							if isAsync {
								ToastError(c, "TLS Invalid", msg)
							}
						}
					}
				}
			}
		} else {
			// When disabling TLS, still allow updating stored paths from provided sources (optional)
			certSrc := tlsCert
			keySrc := tlsKey
			if certSrc != "" && !filepath.IsAbs(certSrc) {
				certSrc = ""
			}
			if keySrc != "" && !filepath.IsAbs(keySrc) {
				keySrc = ""
			}
			if strings.TrimSpace(certSrc) != "" || strings.TrimSpace(keySrc) != "" {
				certRel, keyRel, _ := h.manager.InstallTLSFromSources(certSrc, keySrc)
				if strings.TrimSpace(certRel) != "" {
					h.manager.TLSCertPath = certRel
				}
				if strings.TrimSpace(keyRel) != "" {
					h.manager.TLSKeyPath = keyRel
				}
			}
		}
		h.manager.Save()
		// Apply notification preferences
		h.manager.NotifyEnableDeploy = notifyEnableDeploy
		h.manager.NotifyEnableServer = notifyEnableServer
		h.manager.NotifyDeployOnStarted = notifyDeployOnStarted
		h.manager.NotifyDeployOnCompleted = notifyDeployOnCompleted
		h.manager.NotifyDeployOnCompletedError = notifyDeployOnCompletedError
		h.manager.NotifyOnStart = notifyOnStart
		h.manager.NotifyOnStopping = notifyOnStopping
		h.manager.NotifyOnStopped = notifyOnStopped
		h.manager.NotifyOnRestart = notifyOnRestart
		h.manager.NotifyOnUpdateStarted = notifyOnUpdateStarted
		h.manager.NotifyOnUpdateCompleted = notifyOnUpdateCompleted
		h.manager.NotifyOnUpdateFailed = notifyOnUpdateFailed
		h.manager.NotifyDeployRelease = notifyDeployRelease
		h.manager.NotifyDeployBeta = notifyDeployBeta
		h.manager.NotifyDeployBepInEx = notifyDeployBepInEx
		h.manager.NotifyDeployLaunchPad = notifyDeployLaunchPad
		h.manager.NotifyDeploySCON = notifyDeploySCON
		h.manager.NotifyDeploySteamCMD = notifyDeploySteamCMD
		h.manager.NotifyDeployServers = notifyDeployServers
		// Templates (empty preserves prior value)
		if notifyMsgStart != "" {
			h.manager.NotifyMsgStart = notifyMsgStart
		}
		if notifyMsgStopping != "" {
			h.manager.NotifyMsgStopping = notifyMsgStopping
		}
		if notifyMsgStopped != "" {
			h.manager.NotifyMsgStopped = notifyMsgStopped
		}
		if notifyMsgRestart != "" {
			h.manager.NotifyMsgRestart = notifyMsgRestart
		}
		if notifyMsgUpdateStarted != "" {
			h.manager.NotifyMsgUpdateStarted = notifyMsgUpdateStarted
		}
		if notifyMsgUpdateCompleted != "" {
			h.manager.NotifyMsgUpdateCompleted = notifyMsgUpdateCompleted
		}
		if notifyMsgUpdateFailed != "" {
			h.manager.NotifyMsgUpdateFailed = notifyMsgUpdateFailed
		}
		// Colors (#RRGGBB)
		validColor := func(v string) bool { v = strings.TrimSpace(v); return len(v) == 7 && strings.HasPrefix(v, "#") }
		if validColor(notifyColorStart) {
			h.manager.NotifyColorStart = notifyColorStart
		}
		if validColor(notifyColorStopping) {
			h.manager.NotifyColorStopping = notifyColorStopping
		}
		if validColor(notifyColorStopped) {
			h.manager.NotifyColorStopped = notifyColorStopped
		}
		if validColor(notifyColorRestart) {
			h.manager.NotifyColorRestart = notifyColorRestart
		}
		if validColor(notifyColorUpdateStarted) {
			h.manager.NotifyColorUpdateStarted = notifyColorUpdateStarted
		}
		if validColor(notifyColorUpdateCompleted) {
			h.manager.NotifyColorUpdateCompleted = notifyColorUpdateCompleted
		}
		if validColor(notifyColorUpdateFailed) {
			h.manager.NotifyColorUpdateFailed = notifyColorUpdateFailed
		}
		// Deploy templates/colors
		if notifyMsgDeployStarted != "" {
			h.manager.NotifyMsgDeployStarted = notifyMsgDeployStarted
		}
		if notifyMsgDeployCompleted != "" {
			h.manager.NotifyMsgDeployCompleted = notifyMsgDeployCompleted
		}
		if notifyMsgDeployCompletedError != "" {
			h.manager.NotifyMsgDeployCompletedError = notifyMsgDeployCompletedError
		}
		if validColor(notifyColorDeployStarted) {
			h.manager.NotifyColorDeployStarted = notifyColorDeployStarted
		}
		if validColor(notifyColorDeployCompleted) {
			h.manager.NotifyColorDeployCompleted = notifyColorDeployCompleted
		}
		if validColor(notifyColorDeployCompletedError) {
			h.manager.NotifyColorDeployCompletedError = notifyColorDeployCompletedError
		}
		h.manager.Save()
		// Apply manager port forwarding changes without requiring restart
		if !prevPF && h.manager.AutoPortForwardManager {
			go h.manager.StartManagerPortForwarding()
		} else if prevPF && !h.manager.AutoPortForwardManager {
			h.manager.StopManagerPortForwarding()
		}
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
		// Persist relative managed paths for consistency
		h.manager.TLSCertPath = filepath.ToSlash(filepath.Join("certs", "sdsm.crt"))
		h.manager.TLSKeyPath = filepath.ToSlash(filepath.Join("certs", "sdsm.key"))
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
	if ip6 := net.ParseIP("::1"); ip6 != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip6)
	}
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")
	// Best-effort: include local hostname if available (helps when accessing via machine name)
	if hn, _ := os.Hostname(); strings.TrimSpace(hn) != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, hn)
	}

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
		snapshot := h.manager.ProgressSnapshot()
		c.SSEvent("progress", snapshot)
		// Also emit over WebSocket hub when available so the manager UI can
		// reflect deploy progress in real time without an extra SSE channel.
		if h.hub != nil {
			payload := map[string]any{
				"type":     "manager_progress",
				"snapshot": snapshot,
			}
			if msg, err := json.Marshal(payload); err == nil {
				h.hub.Broadcast(msg)
			}
		}
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

// --- TLS PEM validation helpers (UI-side double check) ---
func validatePEMCertificate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return errors.New("invalid PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return errors.New("certificate not yet valid")
	}
	if now.After(cert.NotAfter) {
		return errors.New("certificate expired")
	}
	return nil
}

func validatePEMKey(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return k, nil
	case "EC PRIVATE KEY":
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return k, nil
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return k, nil
	default:
		return nil, errors.New("unsupported private key type")
	}
}

func keysMatch(certPath string, priv any) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	pub := cert.PublicKey
	switch pk := priv.(type) {
	case *rsa.PrivateKey:
		if rsaPub, ok := pub.(*rsa.PublicKey); ok {
			return rsaPub.N.Cmp(pk.N) == 0
		}
	case *ecdsa.PrivateKey:
		if ecPub, ok := pub.(*ecdsa.PublicKey); ok {
			return ecPub.X.Cmp(pk.X) == 0 && ecPub.Y.Cmp(pk.Y) == 0
		}
	default:
		// Unsupported; assume OK
		return true
	}
	return false
}
