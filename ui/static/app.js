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

    // Theme Management
    theme: {
      getStored: function() {
        try {
          return localStorage.getItem('theme') || 'dark';
        } catch(_) {
          return 'dark';
        }
      },

      set: function(theme) {
        try {
          document.documentElement.setAttribute('data-theme', theme);
          localStorage.setItem('theme', theme);
          const icon = document.getElementById('theme-icon');
          if (icon) {
            icon.textContent = theme === 'dark' ? '☀️' : '🌙';
          }
        } catch(e) {
          console.error('Theme set error:', e);
        }
      },

      toggle: function() {
        const current = document.documentElement.getAttribute('data-theme') || this.getStored();
        this.set(current === 'dark' ? 'light' : 'dark');
      },

      init: function() {
        this.set(this.getStored());
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
      connect: function() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        SDSM.state.ws = new WebSocket(wsUrl);

        SDSM.state.ws.onopen = () => {
          console.log('WebSocket connected');
        };

        SDSM.state.ws.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data);
            this.handleMessage(data);
          } catch(e) {
            console.error('WebSocket message parse error:', e);
          }
        };

        SDSM.state.ws.onclose = () => {
          console.log('WebSocket closed, reconnecting...');
          setTimeout(() => this.connect(), SDSM.config.wsReconnectDelay);
        };

        SDSM.state.ws.onerror = (err) => {
          console.error('WebSocket error:', err);
        };
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
        if (SDSM.state.ws) {
          SDSM.state.ws.close();
        }
      }
    },

    // UI Update Helpers
    ui: {
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

        // Listen for HTMX navigation to update title and active nav item
        document.body.addEventListener('htmx:nav', (evt) => {
            const path = evt.detail.path;
            if (path) {
                SDSM.frame.updateTitle(path);
                SDSM.frame.updateActiveNav(path);
            }
        });

        // On first load, set the title correctly
        SDSM.frame.updateTitle(window.location.pathname);
        SDSM.frame.updateActiveNav(window.location.pathname);
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
        });
      }

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
