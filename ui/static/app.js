/**
 * SDSM Common JavaScript Utilities
 * Shared functions for theme management, WebSocket, health monitoring, and UI helpers
 */

(function(window) {
  'use strict';

  const SDSM = {
    // Configuration
    config: {
      healthEndpoint: '/healthz',
      healthTimeout: 4000,
      healthIntervalOk: 15000,
      healthIntervalLost: 5000,
      wsReconnectDelay: 3000,
      statsPollInterval: 30000
    },

    // State
    state: {
      ws: null,
      healthTimer: null,
      healthLost: false,
      statsPollTimer: null
    },

    // API helpers
    api: {
      async request(url, options = {}) {
        const {
          method = 'POST',
          body,
          headers = {},
          includeBodyWhenEmpty = false
        } = options;

        const fetchHeaders = {
          Accept: 'application/json',
          'HX-Request': 'true',
          ...headers
        };

        let payload = body;
        if (body && !(body instanceof FormData)) {
          fetchHeaders['Content-Type'] = fetchHeaders['Content-Type'] || 'application/json';
          payload = JSON.stringify(body);
        } else if (!body && includeBodyWhenEmpty) {
          fetchHeaders['Content-Type'] = fetchHeaders['Content-Type'] || 'application/json';
          payload = '{}';
        }

        const response = await fetch(url, {
          method,
          headers: fetchHeaders,
          body: payload,
          credentials: 'same-origin'
        });

        const text = await response.text();
        this.showToastFromHeaders(response);

        if (!response.ok) {
          const message = this.extractErrorMessage(text, response.statusText);
          if (!response.headers.get('X-Toast-Message') && window.showToast) {
            window.showToast('Error', message, 'danger');
          }
          throw new Error(message);
        }

        return this.parseJSON(text);
      },

      async download(url, options = {}) {
        if (!url) return;
        const {
          method = 'GET',
          body,
          headers = {},
          filename,
          includeBodyWhenEmpty = false
        } = options;

        // For simple GET downloads without a payload, let the browser handle streaming directly.
        if (method.toUpperCase() === 'GET' && !body) {
          const anchor = document.createElement('a');
          anchor.href = url;
          anchor.rel = 'noopener';
          anchor.style.display = 'none';
          document.body.appendChild(anchor);
          anchor.click();
          document.body.removeChild(anchor);
          return;
        }

        const fetchHeaders = {
          Accept: 'application/octet-stream',
          'HX-Request': 'true',
          ...headers
        };

        let payload = body;
        if (body && !(body instanceof FormData)) {
          fetchHeaders['Content-Type'] = fetchHeaders['Content-Type'] || 'application/json';
          payload = JSON.stringify(body);
        } else if (!body && includeBodyWhenEmpty) {
          fetchHeaders['Content-Type'] = fetchHeaders['Content-Type'] || 'application/json';
          payload = '{}';
        }

        const response = await fetch(url, {
          method,
          headers: fetchHeaders,
          body: payload,
          credentials: 'same-origin'
        });

        this.showToastFromHeaders(response);

        if (!response.ok) {
          const text = await response.text();
          const message = this.extractErrorMessage(text, response.statusText);
          if (!response.headers.get('X-Toast-Message') && window.showToast) {
            window.showToast('Error', message, 'danger');
          }
          throw new Error(message);
        }

        const blob = await response.blob();
        const suggested = filename || this.filenameFromDisposition(response.headers.get('Content-Disposition')) || 'download.bin';

        const blobUrl = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = blobUrl;
        link.download = suggested;
        link.style.display = 'none';
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        setTimeout(() => {
          window.URL.revokeObjectURL(blobUrl);
        }, 0);
      },

      async downloadWorld(serverId, saveName = '', options = {}) {
        if (!serverId) {
          throw new Error('server id required');
        }
        const query = saveName ? `?name=${encodeURIComponent(saveName)}` : '';
        const url = `/api/servers/${serverId}/world/download${query}`;
        const preferredName = saveName || (options.serverName ? `${options.serverName}.save` : `server-${serverId}.save`);
        return this.download(url, {
          method: 'GET',
          filename: preferredName,
        });
      },

      filenameFromDisposition(disposition) {
        if (!disposition) return '';
        const utfMatch = disposition.match(/filename\*=UTF-8''([^;]+)/i);
        if (utfMatch && utfMatch[1]) {
          try {
            return decodeURIComponent(utfMatch[1]);
          } catch (_) {
            return utfMatch[1];
          }
        }
        const plainMatch = disposition.match(/filename="?([^";]+)"?/i);
        if (plainMatch && plainMatch[1]) {
          return plainMatch[1];
        }
        return '';
      },

      parseJSON(text) {
        if (!text) return {};
        try {
          return JSON.parse(text);
        } catch (_) {
          return text;
        }
      },

      extractErrorMessage(text, fallback) {
        if (!text) return fallback || 'Request failed';
        try {
          const parsed = JSON.parse(text);
          if (parsed && typeof parsed.error === 'string') {
            return parsed.error;
          }
        } catch (_) {
          // ignore parse error
        }
        return text.trim() || fallback || 'Request failed';
      },

      showToastFromHeaders(response) {
        if (!window.showToast) return;
        const message = response.headers.get('X-Toast-Message');
        if (!message) return;
        const title = response.headers.get('X-Toast-Title') || '';
        const type = response.headers.get('X-Toast-Type') || 'info';
        window.showToast(title, message, type);
      }
    },

    // Theme Management
    theme: {
      getStored: function() {
        try {
          const value = localStorage.getItem('theme');
          return value === 'light' || value === 'dark' ? value : null;
        } catch(_) {
          return null;
        }
      },

      getPreferred: function() {
        try {
          if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
            return 'dark';
          }
        } catch(_) {}
        const attr = document.documentElement.getAttribute('data-theme');
        return attr === 'light' ? 'light' : 'dark';
      },

      apply: function(theme, persist = true) {
        const normalized = theme === 'light' ? 'light' : 'dark';
        document.documentElement.setAttribute('data-theme', normalized);
        if (document.body) {
          document.body.setAttribute('data-theme', normalized);
        }
        if (persist) {
          try {
            localStorage.setItem('theme', normalized);
          } catch(_) {}
        }
        this.updateToggle(normalized);
      },

      updateToggle: function(theme) {
        const isDark = theme === 'dark';
        const sun = document.getElementById('theme-icon-light');
        const moon = document.getElementById('theme-icon-dark');
        if (sun) {
          sun.classList.toggle('d-none', !isDark);
        }
        if (moon) {
          moon.classList.toggle('d-none', isDark);
        }
        const toggle = document.getElementById('theme-toggle');
        if (toggle) {
          toggle.setAttribute('aria-pressed', isDark ? 'true' : 'false');
          toggle.setAttribute('aria-label', isDark ? 'Switch to light theme' : 'Switch to dark theme');
          toggle.dataset.theme = theme;
        }
      },

      set: function(theme, persist = true) {
        this.apply(theme, persist);
      },

      toggle: function() {
        const current = document.documentElement.getAttribute('data-theme') || this.getPreferred();
        this.apply(current === 'dark' ? 'light' : 'dark', true);
      },

      init: function() {
        const stored = this.getStored();
        const initial = stored || this.getPreferred();
        this.apply(initial, true);
      }
    },

    // Connection Banner Management
    banner: {
      set: function(active, text) {
        // If in iframe, delegate to parent
        if (window.top && window.top !== window && typeof window.top.setConnectionBanner === 'function') {
          window.top.setConnectionBanner(!!active, text || '');
          return;
        }

        const banner = document.getElementById('connection-banner');
        const bannerText = document.getElementById('connection-banner-text');
        
        if (!banner) return;

        if (active) {
          banner.classList.add('active');
          if (bannerText && text) {
            bannerText.textContent = text;
          }
          document.body.style.paddingTop = '3.5rem';
        } else {
          banner.classList.remove('active');
          document.body.style.paddingTop = '';
        }
      }
    },

    // Health Monitoring
    health: {
      schedule: function(delay) {
        if (SDSM.state.healthTimer) {
          clearTimeout(SDSM.state.healthTimer);
        }
        SDSM.state.healthTimer = setTimeout(() => this.check(), delay);
      },

      check: function() {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), SDSM.config.healthTimeout);

        fetch(SDSM.config.healthEndpoint, {
          cache: 'no-store',
          credentials: 'same-origin',
          signal: controller.signal
        })
        .then(response => {
          clearTimeout(timeoutId);
          if (!response.ok) throw new Error('Health check failed');

          if (SDSM.state.healthLost) {
            SDSM.state.healthLost = false;
            SDSM.banner.set(true, 'Connection restored. Reloading...');
            setTimeout(() => {
              SDSM.banner.set(false);
              window.location.reload();
            }, 1200);
            return;
          }

          SDSM.banner.set(false);
        })
        .catch(() => {
          clearTimeout(timeoutId);
          if (!SDSM.state.healthLost) {
            SDSM.state.healthLost = true;
            SDSM.banner.set(true, 'Connection lost. Attempting to reconnect...');
          }
        })
        .finally(() => {
          this.schedule(SDSM.state.healthLost ? SDSM.config.healthIntervalLost : SDSM.config.healthIntervalOk);
        });
      },

      start: function() {
        const reloadBtn = document.getElementById('connection-banner-action');
        if (reloadBtn) {
          reloadBtn.addEventListener('click', () => window.location.reload());
        }
        this.check();
      }
    },

    // WebSocket Management
    ws: {
      connect: function(forceReconnect) {
        const existing = SDSM.state.ws;
        if (!forceReconnect && existing) {
          const state = existing.readyState;
          if (state === WebSocket.OPEN || state === WebSocket.CONNECTING || state === WebSocket.CLOSING) {
            return existing;
          }
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        const socket = new WebSocket(wsUrl);
        SDSM.state.ws = socket;

        socket.onopen = () => {
          console.log('WebSocket connected');
        };

        socket.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data);
            this.handleMessage(data);
          } catch(e) {
            console.error('WebSocket message parse error:', e);
          }
        };

        socket.onclose = () => {
          console.log('WebSocket closed, reconnecting...');
          if (SDSM.state.ws === socket) {
            SDSM.state.ws = null;
          }
          setTimeout(() => this.connect(), SDSM.config.wsReconnectDelay);
        };

        socket.onerror = (err) => {
          console.error('WebSocket error:', err);
        };

        return socket;
      },

      handleMessage: function(data) {
        switch(data.type) {
          case 'server_status':
          case 'server_update':
            if (typeof SDSM.ui.updateServerStatus === 'function') {
              SDSM.ui.updateServerStatus(data.serverId, data.status);
            }
            break;
          case 'servers_changed':
            this.triggerRefresh();
            break;
          case 'stats_update':
            if (typeof SDSM.ui.updateStats === 'function') {
              SDSM.ui.updateStats(data.stats);
            }
            break;
          case 'manager_progress':
            if (typeof SDSM.ui.updateManagerProgress === 'function') {
              SDSM.ui.updateManagerProgress(data.snapshot);
            }
            break;
          default:
            console.log('Unknown WebSocket message type:', data.type);
        }
      },

      triggerRefresh: function() {
        try {
          if (typeof htmx !== 'undefined') {
            htmx.trigger('#server-grid', 'refresh');
            htmx.trigger('.stats-grid', 'refresh');
          }
        } catch(_) {}
      },

      close: function() {
        if (!SDSM.state.ws) {
          return;
        }
        try {
          SDSM.state.ws.onclose = null;
        } catch (_) {}
        try {
          SDSM.state.ws.onerror = null;
        } catch (_) {}
        SDSM.state.ws.close();
        SDSM.state.ws = null;
      }
    },

    // UI Update Helpers
    ui: {
      updateManagerProgress: function(snapshot) {
        if (!snapshot || !Array.isArray(snapshot.components)) return;

        snapshot.components.forEach(comp => {
          const key = (comp.key || comp.Key || '').toLowerCase();
          if (!key) return;
          const row = document.querySelector(`.versions-row[data-component="${key}"]`);
          if (!row) return;

          const progressEl = row.querySelector(`.progress-fill[data-progress="${key}"]`);
          const pill = row.querySelector('.status-pill');

          if (progressEl && typeof comp.percent === 'number') {
            const pct = Math.max(0, Math.min(100, comp.percent));
            progressEl.style.width = pct + '%';
          }

          if (pill) {
            pill.classList.remove('status-ok', 'status-outdated', 'status-running', 'status-error');
            if (snapshot.updating && comp.running) {
              pill.classList.add('status-running');
              pill.textContent = comp.stage || 'Updating...';
            } else if (comp.error) {
              pill.classList.add('status-error');
              pill.textContent = 'Error';
              pill.title = comp.error;
            }
          }
        });
      },

      updateServerStatus: function(serverId, status) {
        const card = document.querySelector(`[data-server-id="${serverId}"]`);
        if (!card) return;

        const statusBadge = card.querySelector('[data-status]');
        const stormBadge = card.querySelector('[data-storm]');

        if (statusBadge) {
          const isRunning = !!status.running;
          const isStarting = !!status.starting && isRunning;
          const isPaused = !!status.paused && isRunning && !isStarting;

          statusBadge.classList.remove('status-running', 'status-idle', 'is-running', 'is-stopped', 'is-paused', 'is-starting');
          statusBadge.classList.toggle('is-starting', isStarting);
          statusBadge.classList.toggle('is-running', isRunning && !isPaused && !isStarting);
          statusBadge.classList.toggle('is-paused', isRunning && isPaused);
          statusBadge.classList.toggle('is-stopped', !isRunning);

          const label = isStarting ? 'Starting' : (!isRunning ? 'Stopped' : (isPaused ? 'Paused' : 'Running'));
          statusBadge.setAttribute('aria-label', `Status: ${label}`);
          statusBadge.setAttribute('title', label);
        }

        if (stormBadge) {
          const isRunning = !!status.running;
          const isStorming = !!status.storming && isRunning;

          if (isRunning && isStorming) {
            stormBadge.style.display = '';
            stormBadge.classList.remove('is-storm', 'is-clear');
            stormBadge.classList.add('is-storm');
            stormBadge.setAttribute('aria-label', 'Storm');
            stormBadge.setAttribute('title', 'Storm');
            stormBadge.textContent = '';
          } else {
            stormBadge.style.display = 'none';
          }
        }

        if (status.name !== undefined) {
          const nameEl = card.querySelector('[data-server-name]');
          if (nameEl) nameEl.textContent = status.name;
        }

        if (status.port !== undefined) {
          const portEl = card.querySelector('[data-port-value]');
          if (portEl) portEl.textContent = String(status.port);
        }

        if (status.playerCount !== undefined) {
          const pcEl = card.querySelector('[data-player-count]');
          if (pcEl) pcEl.textContent = `${status.playerCount}/${status.maxPlayers || '?'}`;
        }

        if (status.world !== undefined) {
          const wEl = card.querySelector('[data-world-value]');
          if (wEl) wEl.textContent = status.world;
        }
      },

      updateStats: function(stats) {
        if (stats.totalServers !== undefined) {
          const total = document.getElementById('total-servers');
          if (total) total.textContent = stats.totalServers;

          const hTotal = document.getElementById('header-total-servers');
          if (hTotal) hTotal.textContent = stats.totalServers;

          const hSuffix = document.getElementById('header-total-servers-suffix');
          if (hSuffix) hSuffix.textContent = stats.totalServers === 1 ? '' : 's';
        }

        if (stats.activeServers !== undefined) {
          const active = document.getElementById('active-servers');
          if (active) active.textContent = stats.activeServers;

          const hActive = document.getElementById('header-active-servers');
          if (hActive) hActive.textContent = stats.activeServers;
        }

        if (stats.totalPlayers !== undefined) {
          const players = document.getElementById('total-players');
          if (players) players.textContent = stats.totalPlayers;

          const hPlayers = document.getElementById('header-total-players');
          if (hPlayers) hPlayers.textContent = stats.totalPlayers;
        }

        const refresh = document.getElementById('last-refresh');
        if (refresh) {
          refresh.textContent = new Date().toLocaleTimeString();
        }
      },

      bindServerCardNavigation: function(root) {
        const cards = (root || document).querySelectorAll('.server-card');
        cards.forEach(card => {
          if (card.dataset.navigationBound === 'true') return;
          card.dataset.navigationBound = 'true';

          card.addEventListener('click', e => {
            if (e.target.closest('[data-stop-navigation="true"]')) return;
            const url = card.getAttribute('data-target-url');
            if (url) window.location.href = url;
          });
        });
      },

      pollHeaderStats: function() {
        fetch('/api/stats', {
          headers: { 'Accept': 'application/json' },
          credentials: 'same-origin',
          cache: 'no-store'
        })
        .then(r => r.ok ? r.json() : null)
        .then(d => {
          if (d) this.updateStats(d);
        })
        .catch(() => {});
      },

      startStatsPoll: function() {
        this.pollHeaderStats();
        if (SDSM.state.statsPollTimer) {
          clearInterval(SDSM.state.statsPollTimer);
        }
        SDSM.state.statsPollTimer = setInterval(() => {
          this.pollHeaderStats();
        }, SDSM.config.statsPollInterval);
      }
    },

    // Server helpers (dashboard)
    servers: {
      async sendRenameRequest(serverId, name) {
        const payload = { method: 'POST', body: { name } };
        if (SDSM.api && typeof SDSM.api.request === 'function') {
          return SDSM.api.request(`/api/servers/${serverId}/rename`, payload);
        }

        const headers = {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'HX-Request': 'true'
        };
        const response = await fetch(`/api/servers/${serverId}/rename`, {
          method: 'POST',
          headers,
          credentials: 'same-origin',
          body: JSON.stringify({ name })
        });
        if (SDSM.api && typeof SDSM.api.showToastFromHeaders === 'function') {
          SDSM.api.showToastFromHeaders(response);
        }
        if (!response.ok) {
          const text = await response.text();
          throw new Error(text || 'Rename failed');
        }
        try {
          return await response.json();
        } catch (_) {
          return {};
        }
      },

      applyRename(serverId, newName) {
        if (!serverId) return;
        const cards = document.querySelectorAll(`.server-card[data-server-id="${serverId}"]`);
        cards.forEach(card => {
          const nameEl = card.querySelector('[data-server-name]');
          if (nameEl) {
            nameEl.textContent = newName;
          }
          card.querySelectorAll('[data-action="rename-server"]').forEach(btn => {
            btn.dataset.serverName = newName;
          });
        });
      },

      promptRename(serverId, currentName) {
        if (!serverId || !SDSM.modal || typeof SDSM.modal.prompt !== 'function') {
          return;
        }

        SDSM.modal.prompt({
          title: 'Rename Server',
          label: 'New Server Name',
          defaultValue: currentName || '',
          confirmText: 'Rename',
          validate: (value) => {
            const trimmed = (value || '').trim();
            if (!trimmed) {
              return 'Server name cannot be empty.';
            }
            if (trimmed.length > 50) {
              return 'Server name is too long.';
            }
            return true;
          }
        }).then(async (result) => {
          const trimmed = (result || '').trim();
          if (!trimmed || trimmed === (currentName || '').trim()) {
            return;
          }
          try {
            await SDSM.servers.sendRenameRequest(serverId, trimmed);
            SDSM.servers.applyRename(serverId, trimmed);
          } catch (err) {
            console.error('Rename failed:', err);
            if (window.showToast && err?.message) {
              window.showToast('Rename Failed', err.message, 'danger');
            }
          }
        });
      }
    },

    // Form Validation Helpers
    forms: {
      validateInput: function(input, minLength) {
        const value = input.value.trim();
        const isValid = value.length >= (minLength || 1);

        input.classList.toggle('border-danger-500', !isValid && value.length > 0);
        input.classList.toggle('border-success-500', isValid);

        return isValid;
      },

      setLoadingState: function(form, loading) {
        const buttons = form.querySelectorAll('button[type="submit"]');
        buttons.forEach(button => {
          button.disabled = loading;
          if (loading) {
            button.dataset.originalText = button.textContent;
            button.textContent = button.dataset.loadingText || 'Loading...';
          } else {
            button.textContent = button.dataset.originalText || button.textContent;
          }
        });
      }
    },

    // Keyboard Shortcuts
    keyboard: {
      init: function() {
        document.addEventListener('keydown', (e) => {
          // Ctrl+Shift+T: Toggle theme
          if (e.ctrlKey && e.shiftKey && e.key === 'T') {
            e.preventDefault();
            SDSM.theme.toggle();
          }

          // Ctrl+R: Refresh data (if htmx available)
          if (e.ctrlKey && e.key === 'r' && typeof htmx !== 'undefined') {
            e.preventDefault();
            try {
              htmx.trigger('#server-grid', 'refresh');
            } catch(_) {}
          }

          // Escape: Close modals
          if (e.key === 'Escape') {
            const activeModal = document.querySelector('.confirm-modal.active');
            if (activeModal) {
              const closeBtn = activeModal.querySelector('[data-close]') || activeModal.querySelector('[data-cancel]');
              if (closeBtn) closeBtn.click();
            }
          }
        });
      }
    },

    // Frame Management
    frame: {
      pageTitles: {
        '/dashboard': 'Dashboard',
        '/manager': 'Manager Settings',
        '/help/tokens': 'Chat Tokens',
      },

      updateTitle: (path) => {
        const titleEl = document.getElementById('page-title');
        if (!titleEl) return;

        if (/^\/server\/(\d+)$/.test(path)) {
            titleEl.textContent = 'Server ' + path.replace(/^\/server\//, '');
        } else {
            titleEl.textContent = SDSM.frame.pageTitles[path] || path;
        }
      },

      updateActiveNav: (path) => {
        document.querySelectorAll('.nav-item').forEach(item => item.classList.remove('active'));
        const activeItem = document.querySelector(`.nav-item[data-target="${path}"]`);
        if (activeItem) {
            activeItem.classList.add('active');
        }
      },

      init: () => {
        // Update date/time display
        const dtEl = document.getElementById('datetime');
        if (dtEl) {
            const updateDateTime = () => {
                dtEl.textContent = new Date().toLocaleString('en-US', {
                    weekday: 'short',
                    year: 'numeric',
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit'
                });
            };
            updateDateTime();
            setInterval(updateDateTime, 1000);
        }

        const syncNavState = (path) => {
          const targetPath = path || window.location.pathname;
          SDSM.frame.updateTitle(targetPath);
          SDSM.frame.updateActiveNav(targetPath);
        };

        // Listen for HTMX navigation to update title and active nav item
        if (typeof htmx !== 'undefined') {
          document.body.addEventListener('htmx:pushedIntoHistory', (evt) => {
            syncNavState(evt.detail?.path);
          });

          document.body.addEventListener('htmx:historyRestore', (evt) => {
            syncNavState(evt.detail?.path);
          });

          document.body.addEventListener('htmx:afterSwap', (evt) => {
            if (evt.target && evt.target.id === 'content-area') {
              syncNavState(window.location.pathname);
            }
          });
        }

        window.addEventListener('popstate', () => syncNavState(window.location.pathname));

        document.body.addEventListener('click', (evt) => {
          const target = evt.target instanceof Element ? evt.target.closest('.nav-item') : null;
          if (target && target.dataset.target) {
            SDSM.frame.updateActiveNav(target.dataset.target);
          }
        });

        // On first load, set the title correctly
        syncNavState(window.location.pathname);
      }
    },

    // Initialization
    init: function() {
      // Initialize theme immediately
      this.theme.init();

      // Wait for DOM ready for everything else
      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => this.onReady());
      } else {
        this.onReady();
      }
    },

    onReady: function() {
      // Initialize keyboard shortcuts
      this.keyboard.init();

      const themeToggleBtn = document.getElementById('theme-toggle');
      if (themeToggleBtn && !themeToggleBtn.dataset.boundThemeToggle) {
        themeToggleBtn.dataset.boundThemeToggle = 'true';
        themeToggleBtn.addEventListener('click', (event) => {
          event.preventDefault();
          SDSM.theme.toggle();
        });
      }

      // Ensure the shared WebSocket connection is active so realtime UI
      // updates (manager progress, server statuses, etc.) are received even on
      // pages that do not run page-specific scripts.
      try {
        if (typeof WebSocket !== 'undefined') {
          const ws = this.state.ws;
          const needsConnect = !ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING;
          if (needsConnect) {
            this.ws.connect();
          }
        }
      } catch (err) {
        console.error('WebSocket initialization failed:', err);
      }

      // Initialize HTMX indicators if HTMX is present
      if (typeof htmx !== 'undefined') {
        document.body.addEventListener('htmx:beforeRequest', (ev) => {
          if (ev.detail.elt?.getAttribute('hx-indicator') === '#refresh-indicator') {
            const indicator = document.getElementById('refresh-indicator');
            if (indicator) indicator.classList.remove('hidden');
          }
        });

        document.body.addEventListener('htmx:afterRequest', (ev) => {
          if (ev.detail.elt?.getAttribute('hx-indicator') === '#refresh-indicator') {
            const indicator = document.getElementById('refresh-indicator');
            if (indicator) indicator.classList.add('hidden');
          }
        });

        document.addEventListener('htmx:afterSwap', (e) => {
          if (e.target && e.target.closest('#server-grid')) {
            this.ui.bindServerCardNavigation(e.target.closest('#server-grid'));
          }

          // When #content-area is swapped, update the frame title from the
          // new content's data-page-title attribute if present.
          if (e.target && e.target.id === 'content-area') {
            const titleEl = document.getElementById('page-title');
            if (titleEl) {
              const wrapper = e.target.querySelector('[data-page-title]');
              if (wrapper && wrapper.dataset.pageTitle) {
                titleEl.textContent = wrapper.dataset.pageTitle;
              }
            }
          }
        });
      }

      document.body.addEventListener('click', (event) => {
        const renameBtn = event.target.closest('[data-action="rename-server"]');
        if (!renameBtn) {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        if (SDSM.servers && typeof SDSM.servers.promptRename === 'function') {
          const serverId = renameBtn.dataset.serverId;
          const serverName = renameBtn.dataset.serverName || '';
          SDSM.servers.promptRename(serverId, serverName);
        }
      });

      // Cleanup on page unload
      window.addEventListener('beforeunload', () => {
        this.ws.close();
        if (this.state.healthTimer) {
          clearTimeout(this.state.healthTimer);
        }
        if (this.state.statsPollTimer) {
          clearInterval(this.state.statsPollTimer);
        }
      });
    }
  };

  // Export to global scope
  window.SDSM = SDSM;

  // Auto-initialize
  SDSM.init();

  // Legacy compatibility - expose common functions at window level
  window.toggleTheme = function() { SDSM.theme.toggle(); };
  window.getStoredTheme = function() { return SDSM.theme.getStored(); };
  window.setTheme = function(theme) { SDSM.theme.set(theme); };

})(window);

/**
 * Page specific scripts
 */

document.addEventListener('DOMContentLoaded', function () {
    // Profile page
    if (document.getElementById('profile-form')) {
        const profileForm = document.getElementById('profile-form');
        if (!profileForm) return;

        const newPassword = document.getElementById('new_password');
        const confirmPassword = document.getElementById('confirm_password');
        const passwordHint = document.getElementById('password-hint');
        const submitButton = document.getElementById('btn-update-password');

        function validatePasswords() {
            if (newPassword.value !== confirmPassword.value) {
                passwordHint.textContent = 'Passwords do not match.';
                confirmPassword.setCustomValidity("Passwords Don't Match");
                submitButton.disabled = true;
            return false;
            } else if (newPassword.value.length > 0 && newPassword.value.length < 8) {
                passwordHint.textContent = 'Password must be at least 8 characters long.';
                newPassword.setCustomValidity("Password must be at least 8 characters long.");
                submitButton.disabled = true;
            return false;
            } else {
                passwordHint.textContent = '';
                confirmPassword.setCustomValidity('');
                newPassword.setCustomValidity('');
                submitButton.disabled = false;
            return true;
            }
        }

        if (newPassword && confirmPassword) {
            newPassword.addEventListener('input', validatePasswords);
            confirmPassword.addEventListener('input', validatePasswords);
        }

        function setSubmitting(submitting) {
          if (!submitButton) return;
          if (submitting) {
            submitButton.dataset.originalText = submitButton.dataset.originalText || submitButton.textContent;
            submitButton.textContent = submitButton.dataset.loadingText || 'Updating...';
          } else if (submitButton.dataset.originalText) {
            submitButton.textContent = submitButton.dataset.originalText;
          }
          submitButton.disabled = submitting;
        }

        profileForm.addEventListener('submit', async (event) => {
          event.preventDefault();
          if (submitButton.disabled) return;
          if (!validatePasswords()) {
            return;
          }

          const payload = {
            current_password: document.getElementById('current_password').value,
            new_password: newPassword.value,
            confirm_password: confirmPassword.value
          };

          setSubmitting(true);
          try {
            await SDSM.api.request('/api/profile/password', { method: 'POST', body: payload });
            showToast('Success', 'Password updated successfully.', 'success');
            profileForm.reset();
            validatePasswords();
          } catch (err) {
            showToast('Error', err.message || 'Failed to update password.', 'danger');
          } finally {
            setSubmitting(false);
          }
        });
    }

    // Server new page
    if (document.getElementById('serverForm')) {
        const form = document.getElementById('serverForm');
        if (!form) return;

        // Dropzone
        const dropzone = document.getElementById('saveDropzone');
        const saveFileInput = document.getElementById('save_file');
        const saveFileName = document.getElementById('saveFileName');
        const saveAnalysis = document.getElementById('saveAnalysis');
        const detectedWorld = document.getElementById('detectedWorld');
        const detectedName = document.getElementById('detectedName');
        const basicNameGroup = document.getElementById('basicNameGroup');
        const basicWorldGroup = document.getElementById('basicWorldGroup');
        const nameInput = document.getElementById('name');
        const worldInput = document.getElementById('world');
        const nameTextInput = document.getElementById('name_text');
        const worldSelect = document.getElementById('world_select');

        // Progress
        const progressFill = document.getElementById('progressFill');
        const progressText = document.getElementById('progressText');
        const progressList = document.getElementById('progressList');
        const requiredFields = ['name', 'world', 'start_location', 'start_condition', 'difficulty'];
        const totalRequired = requiredFields.length;

        function handleFileSelect(file) {
            if (!file) return;
            
            saveFileName.textContent = file.name;
            dropzone.classList.add('dz-started');

            const formData = new FormData();
            formData.append('save_file', file);

            htmx.ajax('POST', '/api/server/analyze-save', {
                body: formData,
                swap: 'none'
            }).then(e => {
                if (e.detail.xhr.status === 200) {
                    const data = JSON.parse(e.detail.xhr.responseText);
                    detectedWorld.textContent = data.world;
                    detectedName.textContent = data.name;
                    
                    nameInput.value = data.name;
                    worldInput.value = data.world;
                    nameTextInput.value = data.name;
                    
                    // Find and select the option in the world dropdown
                    const worldOption = Array.from(worldSelect.options).find(opt => opt.value === data.world);
                    if (worldOption) {
                        worldOption.selected = true;
                    } else {
                        // If the world is not in the list, add it
                        const newOption = new Option(data.world, data.world, true, true);
                        worldSelect.add(newOption);
                    }

                    saveAnalysis.classList.remove('hidden');
                    basicNameGroup.classList.add('hidden');
                    basicWorldGroup.classList.add('hidden');
                    updateProgress();
                } else {
                    const error = JSON.parse(e.detail.xhr.responseText);
                    showToast('Error analyzing save', error.message, 'danger');
                    resetDropzone();
                }
            }).catch(() => {
                showToast('Error', 'An unexpected error occurred while analyzing the save file.', 'danger');
                resetDropzone();
            });
        }
        
        function resetDropzone() {
            saveFileInput.value = '';
            saveFileName.textContent = '';
            dropzone.classList.remove('dz-started');
            saveAnalysis.classList.add('hidden');
            basicNameGroup.classList.remove('hidden');
            basicWorldGroup.classList.remove('hidden');
            nameInput.value = '';
            worldInput.value = '';
            updateProgress();
        }

        dropzone.addEventListener('click', () => saveFileInput.click());
        saveFileInput.addEventListener('change', () => handleFileSelect(saveFileInput.files[0]));
        dropzone.addEventListener('dragover', (e) => e.preventDefault());
        dropzone.addEventListener('drop', (e) => {
            e.preventDefault();
            handleFileSelect(e.dataTransfer.files[0]);
        });

        // Populate dropdowns
        function populateSelect(elementId, url, placeholder) {
            const select = document.getElementById(elementId);
            if (!select) return;
            
            fetch(url)
                .then(response => response.json())
                .then(data => {
                    select.innerHTML = `<option value="">${placeholder}</option>`;
                    data.forEach(item => {
                        const option = new Option(item.name, item.value);
                        select.add(option);
                    });
                })
                .catch(error => console.error(`Error fetching data for ${elementId}:`, error));
        }

        populateSelect('world_select', '/api/data/worlds', 'Select a world');
        populateSelect('start_location', '/api/data/startlocations', 'Select a start location');
        populateSelect('start_condition', '/api/data/startconditions', 'Select a start condition');

        // Form submission
        form.addEventListener('submit', function(e) {
            e.preventDefault();
            const formData = new FormData(form);
            
            // If a save was used, ensure the hidden name/world are used
            if (!basicNameGroup.classList.contains('hidden')) {
                formData.set('name', nameTextInput.value);
            }
            if (!basicWorldGroup.classList.contains('hidden')) {
                formData.set('world', worldSelect.value);
            }

            htmx.ajax('POST', '/server/new', {
                body: formData,
                target: 'body',
                pushUrl: true
            }).catch(err => {
                console.error("Form submission error", err);
                showToast('Error', 'Failed to create server. Check the form for errors.', 'danger');
            });
        });

        // Progress tracking
        function updateProgress() {
            let completedCount = 0;
            const formData = new FormData(form);

            // Handle save file case
            if (!basicNameGroup.classList.contains('hidden')) {
                formData.set('name', nameTextInput.value);
            }
            if (!basicWorldGroup.classList.contains('hidden')) {
                formData.set('world', worldSelect.value);
            }

            requiredFields.forEach(fieldName => {
                const value = formData.get(fieldName);
                const progressItem = progressList.querySelector(`li[data-field="${fieldName}"]`);
                if (value) {
                    completedCount++;
                    progressItem.classList.add('completed');
                } else {
                    progressItem.classList.remove('completed');
                }
            });

            const percentage = totalRequired > 0 ? (completedCount / totalRequired) * 100 : 0;
            progressFill.style.width = `${percentage}%`;
            progressText.textContent = `${completedCount} of ${totalRequired} required fields complete`;
        }

        form.addEventListener('input', updateProgress);
        
        // Initial check
        updateProgress();
    }

    // Commands page
    if (document.getElementById('commands-table')) {
        const searchInput = document.getElementById('cmd-search');
        const table = document.getElementById('commands-table');
        const filterInfo = document.getElementById('filter-info');
        const totalCommands = table.tBodies[0].rows.length;

        if (!searchInput || !table) return;

        function filterCommands() {
            const query = searchInput.value.toLowerCase().trim();
            let visibleCount = 0;

            for (const row of table.tBodies[0].rows) {
                const name = row.dataset.name.toLowerCase();
                const usage = row.dataset.usage.toLowerCase();
                const desc = row.dataset.desc.toLowerCase();
                
                if (name.includes(query) || usage.includes(query) || desc.includes(query)) {
                    row.style.display = '';
                    visibleCount++;
                } else {
                    row.style.display = 'none';
                }
            }
            filterInfo.textContent = `Showing ${visibleCount} of ${totalCommands} commands. Click a command name to copy it.`;
        }

        function copyCommand(event) {
            const target = event.target.closest('.cmd-name');
            if (!target) return;

            const command = target.querySelector('code').textContent;
            navigator.clipboard.writeText(command).then(() => {
                showToast('Copied!', `Command "${command}" copied to clipboard.`, 'success');
            }).catch(err => {
                console.error('Failed to copy command: ', err);
                showToast('Error', 'Failed to copy command.', 'danger');
            });
        }

        searchInput.addEventListener('input', filterCommands);
        table.addEventListener('click', copyCommand);

        // Truncate long descriptions
        document.querySelectorAll('.description-block').forEach(block => {
            const lineHeight = parseInt(window.getComputedStyle(block).lineHeight);
            const maxHeight = lineHeight * 3; // Max 3 lines
            if (block.scrollHeight > maxHeight) {
                block.style.maxHeight = `${maxHeight}px`;
                block.classList.add('truncated');
                const toggle = block.nextElementSibling;
                if (toggle && toggle.classList.contains('desc-toggle')) {
                    toggle.classList.remove('hidden');
                    toggle.addEventListener('click', (e) => {
                        e.preventDefault();
                        block.style.maxHeight = '';
                        block.classList.remove('truncated');
                        toggle.classList.add('hidden');
                    });
                }
            }
        });
    }

    // Logs page
    if (document.getElementById('logTabs')) {
        const logTabs = document.getElementById('logTabs');
        const logTabsEmpty = document.getElementById('log-tabs-empty');
        const logContent = document.getElementById('logContent');
        const logMeta = document.getElementById('logMeta');
        const refreshBtn = document.getElementById('refreshBtn');
        const downloadBtn = document.getElementById('downloadBtn');
        const clearBtn = document.getElementById('clearBtn');

        let activeLogFile = null;

        function fetchLogFiles() {
            htmx.ajax('GET', '/api/logs', {
                target: '#logTabs',
                swap: 'innerHTML'
            }).then((e) => {
                if (e.detail.xhr.status < 400) {
                    if (logTabs.children.length > 1) { // more than just the empty state
                        logTabsEmpty.classList.add('hidden');
                        const firstLog = logTabs.querySelector('.tab');
                        if (firstLog) {
                            firstLog.click();
                        }
                    } else {
                        logTabsEmpty.classList.remove('hidden');
                        logContent.innerHTML = '<div class="empty-state">No log files found.</div>';
                        logMeta.innerHTML = '';
                        activeLogFile = null;
                        updateButtonStates();
                    }
                }
            });
        }

        function fetchLogContent(logFile) {
            activeLogFile = logFile;
            htmx.ajax('GET', `/api/logs/${logFile}`, {
                target: '#logContent',
                swap: 'innerHTML'
            });
            htmx.ajax('GET', `/api/logs/${logFile}/meta`, {
                target: '#logMeta',
                swap: 'innerHTML'
            });
            updateButtonStates();
        }

        function updateButtonStates() {
            const hasActiveLog = activeLogFile !== null;
            if(downloadBtn) downloadBtn.disabled = !hasActiveLog;
            if(clearBtn) clearBtn.disabled = !hasActiveLog;
        }

        if (logTabs) {
            logTabs.addEventListener('click', (e) => {
                const tab = e.target.closest('.tab');
                if (tab) {
                    if (logTabs.querySelector('.active')) {
                        logTabs.querySelector('.active').classList.remove('active');
                    }
                    tab.classList.add('active');
                    const logFile = tab.dataset.logFile;
                    fetchLogContent(logFile);
                }
            });
        }

        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => {
                if (activeLogFile) {
                    fetchLogContent(activeLogFile);
                } else {
                    fetchLogFiles();
                }
            });
        }

        if (downloadBtn) {
            downloadBtn.addEventListener('click', () => {
                if (activeLogFile) {
                    window.location.href = `/api/logs/${activeLogFile}?download=true`;
                }
            });
        }

        if (clearBtn) {
            clearBtn.addEventListener('click', () => {
                if (activeLogFile && confirm(`Are you sure you want to clear the log file "${activeLogFile}"?`)) {
                    htmx.ajax('POST', `/api/logs/${activeLogFile}/clear`, {}).then(() => {
                        fetchLogContent(activeLogFile);
                    });
                }
            });
        }

        // Initial load
        fetchLogFiles();
        updateButtonStates();
    }
});
