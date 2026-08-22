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
      statsPollTimer: null,
      role: '',
      username: '',
      page: '',
      buildTime: '',
      lastStats: null,
      managerWasUpdating: false
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

    // Relative time helpers for uptime/downtime badges
    relativeTime: {
      intervalId: null,
      selector: '[data-relative-mode][data-relative-source]',

      formatDuration(seconds) {
        if (!Number.isFinite(seconds) || seconds <= 0) {
          return '0m';
        }
        const day = 86400;
        const hour = 3600;
        const minute = 60;
        if (seconds >= day) {
          const days = Math.floor(seconds / day);
          const hours = Math.floor((seconds % day) / hour);
          return `${days}d ${hours.toString().padStart(2, '0')}h`;
        }
        if (seconds >= hour) {
          const hours = Math.floor(seconds / hour);
          const minutes = Math.floor((seconds % hour) / minute);
          return `${hours}h ${minutes.toString().padStart(2, '0')}m`;
        }
        const minutes = Math.floor(seconds / minute);
        if (minutes > 0) {
          return `${minutes}m`;
        }
        return `${Math.max(1, Math.floor(seconds))}s`;
      },

      render(el) {
        if (!el || !el.dataset) {
          return;
        }
        const mode = el.dataset.relativeMode;
        const source = el.dataset.relativeSource;
        if (!mode || !source) {
          return;
        }
        const parsed = Date.parse(source);
        if (Number.isNaN(parsed)) {
          return;
        }
        const diffSeconds = Math.max(0, Math.floor((Date.now() - parsed) / 1000));
        const label = this.formatDuration(diffSeconds);
        const prefix = (el.dataset.relativePrefix || '').trim();
        const suffix = (el.dataset.relativeSuffix || '').trim();
        let text = label;
        if (prefix) {
          text = `${prefix} ${text}`.trim();
        }
        if (suffix) {
          text = `${text} ${suffix}`.trim();
        }
        el.textContent = text;
      },

      refresh(root) {
        const scope = root && root.querySelectorAll ? root : document;
        scope.querySelectorAll(this.selector).forEach((el) => this.render(el));
      },

      init() {
        if (this.intervalId) {
          return;
        }
        this.refresh();
        this.intervalId = setInterval(() => this.refresh(), 60000);
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
      formatBytes: function(bytes) {
        const value = Number(bytes);
        if (!Number.isFinite(value) || value <= 0) {
          return '';
        }
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        let size = value;
        let unitIndex = 0;
        while (size >= 1024 && unitIndex < units.length - 1) {
          size /= 1024;
          unitIndex++;
        }
        const precision = unitIndex <= 1 ? 0 : 1;
        return `${size.toFixed(precision)} ${units[unitIndex]}`;
      },

      formatRate: function(bytesPerSecond) {
        const value = Number(bytesPerSecond);
        if (!Number.isFinite(value) || value <= 0) {
          return '';
        }
        const pretty = this.formatBytes(value);
        return pretty ? `${pretty}/s` : '';
      },

      formatPercent: function(value, digits = 0, fallback = '—', suffix = '%') {
        const clamped = this.clampPercent(value);
        if (clamped === null) {
          return fallback;
        }
        const decimals = Math.max(0, digits | 0);
        const label = decimals > 0 ? clamped.toFixed(decimals) : Math.round(clamped).toString();
        return suffix ? `${label}${suffix}` : label;
      },

      clampPercent: function(value) {
        const num = Number(value);
        if (!Number.isFinite(num)) {
          return null;
        }
        if (num < 0) {
          return 0;
        }
        if (num > 100) {
          return 100;
        }
        return num;
      },

      formatLoadAverage: function(load1, load5, load15, fallback = '—') {
        const samples = [load1, load5, load15];
        let hasValue = false;
        const parts = samples.map((entry) => {
          const num = Number(entry);
          if (Number.isFinite(num)) {
            hasValue = true;
            return num.toFixed(2);
          }
          return '—';
        });
        if (!hasValue) {
          return fallback;
        }
        return parts.join(' / ');
      },

      formatUptime: function(seconds, fallback = '') {
        const value = Number(seconds);
        if (!Number.isFinite(value) || value <= 0) {
          return fallback;
        }
        if (SDSM.relativeTime && typeof SDSM.relativeTime.formatDuration === 'function') {
          return SDSM.relativeTime.formatDuration(value);
        }
        const minutes = Math.floor(value / 60);
        if (minutes > 0) {
          return `${minutes}m`;
        }
        return `${Math.max(1, Math.floor(value))}s`;
      },

      formatUsageDetail: function(used, total) {
        const usedLabel = this.formatBytes(used);
        const totalLabel = this.formatBytes(total);
        if (usedLabel && totalLabel) {
          return `${usedLabel} / ${totalLabel}`;
        }
        return usedLabel || totalLabel || '';
      },

      formatRateWithTotal: function(rate, total) {
        const rateLabel = this.formatRate(rate);
        const totalLabel = this.formatBytes(total);
        if (rateLabel && totalLabel) {
          return `${rateLabel} · ${totalLabel}`;
        }
        return rateLabel || totalLabel || '';
      },

      healthStateForPercent: function(percent, options = {}) {
        const telemetryAvailable = !!options.telemetryAvailable;
        const fallbackLabel = (options.fallbackLabel || '').trim();
        const clamped = this.clampPercent(percent);

        if (!telemetryAvailable) {
          return {
            label: fallbackLabel || 'Offline',
            className: 'is-offline'
          };
        }

        if (clamped === null) {
          return {
            label: fallbackLabel || 'Unknown',
            className: 'is-warning'
          };
        }

        if (clamped >= 85) {
          return { label: 'Healthy', className: 'is-healthy' };
        }
        if (clamped >= 65) {
          return { label: 'Stable', className: 'is-stable' };
        }
        if (clamped >= 40) {
          return { label: 'Watch', className: 'is-warning' };
        }
        return { label: 'Critical', className: 'is-critical' };
      },

      updateUsageBars: function(card, usage) {
        if (!card) return;
        const clamp = (value) => Math.max(0, Math.min(100, Number(value) || 0));
        const cpuBar = card.querySelector('[data-cpu-bar]');
        const cpuText = card.querySelector('[data-cpu-text]');
        const memBar = card.querySelector('[data-memory-bar]');
        const memText = card.querySelector('[data-memory-text]');

        if (cpuBar) {
          const pct = clamp(usage?.cpuPercent);
          cpuBar.style.width = `${pct}%`;
          if (pct === 0 && !usage?.cpuPercent) {
            cpuBar.style.opacity = 0.45;
          } else {
            cpuBar.style.opacity = 1;
          }
          if (cpuText) {
            cpuText.textContent = pct > 0 ? `${Math.round(pct)}%` : '—';
          }
        } else if (cpuText) {
          const pct = clamp(usage?.cpuPercent);
          cpuText.textContent = pct > 0 ? `${Math.round(pct)}%` : '—';
        }

        if (memBar || memText) {
          const pct = clamp(usage?.memoryPercent);
          const bytes = Number(usage?.memoryBytes) || 0;
          if (memBar) {
            memBar.style.width = `${pct}%`;
            memBar.style.opacity = pct > 0 ? 1 : 0.45;
          }
          if (memText) {
            let label = pct > 0 ? `${Math.round(pct)}%` : '—';
            const pretty = this.formatBytes(bytes);
            if (pretty) {
              label = `${label} (${pretty})`;
            }
            memText.textContent = label;
            if (pretty) {
              memText.setAttribute('title', `${pct.toFixed(1)}% · ${pretty}`);
            }
          }
        }
      },

      summarizeUpdateError: function(comp) {
        const raw = (comp && (comp.errorSummary || comp.error || '') ? String(comp.errorSummary || comp.error || '') : '').trim();
        if (!raw) return 'Update failed';
        const oneLine = raw.split(/\r?\n/)[0].trim().replace(/\s+/g, ' ');
        if (oneLine.length <= 120) return oneLine;
        return oneLine.slice(0, 119).replace(/\s+$/, '') + '…';
      },

      inferUpdateRecommendation: function(errorText) {
        const text = String(errorText || '').toLowerCase();
        if (!text) return '';

        const includesAny = (...values) => values.some((value) => text.includes(String(value || '').toLowerCase()));

        if (includesAny('no space left on device', 'not enough space', 'disk full')) {
          return 'Free disk space on the SDSM drive, then retry the update.';
        }
        if (includesAny('permission denied', 'operation not permitted', 'access is denied')) {
          return 'Grant write access to the SDSM root path and component folders, then retry.';
        }
        if (includesAny('dial tcp', 'i/o timeout', 'tls handshake timeout', 'temporary failure in name resolution', 'no such host', 'connection refused', 'connection reset', 'no route to host')) {
          return 'Check internet, DNS, firewall/proxy settings, then retry.';
        }
        if (includesAny('failed to download', 'too many requests', 'status 429', 'status 403', 'service unavailable')) {
          return 'Upstream download failed. Retry shortly; if this repeats, check connectivity and remote rate limits.';
        }
        if (includesAny('zip', 'gzip', 'tar', 'archive', 'unexpected eof')) {
          return 'Archive may be corrupted. Retry update; if it repeats, clear component files and retry.';
        }
        if (includesAny('executable file not found', 'steamcmd executable missing', 'no such file or directory')) {
          return 'A required file/executable is missing. Update SteamCMD first, then retry this component.';
        }
        if (includesAny('invalid steamid', 'invalid steam id')) {
          return 'Set a valid numeric Steam ID in Manager settings, save, and retry.';
        }

        return 'Check Update Log for full details, resolve the root cause, then retry this component.';
      },

      updateFailureDetails: function(comp) {
        if (!comp) return { summary: 'Update failed', recommendation: '' };
        const summary = this.summarizeUpdateError(comp);
        const recommendation = String(comp.recommendation || '').trim() || this.inferUpdateRecommendation(comp.error || summary);
        return { summary, recommendation };
      },

      syncVersionActionState: function(panel, updating) {
        if (!panel) return;

        let hasOutdated = false;

        panel.querySelectorAll('.versions-row').forEach((row) => {
          const btn = row.querySelector('.version-action');
          if (!btn) return;

          const isOutdated = (row.dataset.outdated || '').toLowerCase() === 'true';
          const label = btn.querySelector('span');

          if (isOutdated) {
            hasOutdated = true;
          }

          if (updating) {
            btn.disabled = true;
            return;
          }

          if (isOutdated) {
            btn.disabled = false;
            btn.removeAttribute('aria-disabled');
            btn.removeAttribute('title');
            if (label) label.textContent = 'Update';
          } else {
            btn.disabled = true;
            btn.setAttribute('aria-disabled', 'true');
            btn.setAttribute('title', 'Already up to date');
            if (label) label.textContent = 'Current';
          }
        });

        const updateAllBtn = panel.querySelector('.update-all-btn');
        if (updateAllBtn) {
          if (updating) {
            updateAllBtn.disabled = true;
            return;
          }

          updateAllBtn.disabled = !hasOutdated;
          if (hasOutdated) {
            updateAllBtn.removeAttribute('aria-disabled');
            updateAllBtn.removeAttribute('title');
            updateAllBtn.textContent = 'Update All';
          } else {
            updateAllBtn.setAttribute('aria-disabled', 'true');
            updateAllBtn.setAttribute('title', 'All components are up to date');
            updateAllBtn.textContent = 'All Current';
          }
        }
      },

      updateManagerProgress: function(snapshot) {
        const panel = document.querySelector('.versions-panel');
        const updateMessage = document.getElementById('updateMessage');
        const updateMessageDetails = document.getElementById('updateMessageDetails');
        const updateMessageError = document.getElementById('updateMessageError');
        const updateMessageAction = document.getElementById('updateMessageAction');
        const wasUpdating = !!SDSM.state.managerWasUpdating;

        const resetDetails = () => {
          if (!updateMessageDetails) return;
          updateMessageDetails.hidden = true;
          updateMessageDetails.open = false;
          if (updateMessageError) updateMessageError.textContent = '';
          if (updateMessageAction) updateMessageAction.textContent = '';
        };

        if (!snapshot || !Array.isArray(snapshot.components)) {
          if (panel) {
            panel.dataset.versionUpdating = 'false';
            this.syncVersionActionState(panel, false);
          }
          if (updateMessage) {
            updateMessage.textContent = '';
            updateMessage.classList.remove('active', 'error');
          }
          resetDetails();
          SDSM.state.managerWasUpdating = false;
          return;
        }

        const components = snapshot.components.filter(Boolean);
        const updating = !!snapshot.updating;
        if (panel) {
          panel.dataset.versionUpdating = updating ? 'true' : 'false';
          this.syncVersionActionState(panel, updating);
        }

        if (wasUpdating && !updating && typeof htmx !== 'undefined' && document.body) {
          htmx.trigger(document.body, 'sdsm:refresh-versions');
        }
        SDSM.state.managerWasUpdating = updating;

        let activeComponent = null;
        let failedComponent = null;

        components.forEach(comp => {
          const key = (comp.key || comp.Key || '').toLowerCase();
          if (!key) return;
          const row = document.querySelector(`.versions-row[data-component="${key}"]`);
          if (!row) return;

          const progressEl = row.querySelector(`.progress-fill[data-progress="${key}"]`);
          const statusLabel = row.querySelector('[data-progress-label]');
          const isOutdated = (row.dataset.outdated || '').toLowerCase() === 'true';

          if (progressEl && typeof comp.percent === 'number') {
            const pct = Math.max(0, Math.min(100, comp.percent));
            progressEl.style.width = pct + '%';
          }

          if (comp.running) {
            activeComponent = comp;
          }
          if (comp.error) {
            failedComponent = comp;
          }

          if (!statusLabel) {
            return;
          }

          const updateLabel = (state, text, title) => {
            statusLabel.dataset.state = state;
            statusLabel.textContent = text;
            if (title) {
              statusLabel.title = title;
            } else {
              statusLabel.removeAttribute('title');
            }
          };

          if (updating && comp.running) {
            updateLabel('running', comp.stage || 'Updating...', '');
          } else if (comp.error) {
            const details = this.updateFailureDetails(comp);
            const label = `Error: ${details.summary}`;
            const title = details.recommendation
              ? `${comp.error || details.summary}\nRecommended action: ${details.recommendation}`
              : (comp.error || details.summary);
            updateLabel('error', label, title);
          } else if (isOutdated) {
            updateLabel('outdated', 'Update available', '');
          } else {
            updateLabel('complete', 'Completed', '');
          }
        });

        if (updateMessage) {
          if (failedComponent) {
            const details = this.updateFailureDetails(failedComponent);
            updateMessage.classList.add('active', 'error');
            const componentName = failedComponent.displayName || failedComponent.component || 'Component';
            updateMessage.textContent = details.recommendation
              ? `${componentName} failed: ${details.summary} — Recommended action: ${details.recommendation}`
              : `${componentName} failed: ${details.summary}`;

            if (updateMessageDetails) {
              updateMessageDetails.hidden = false;
              const rawError = String(failedComponent.error || details.summary || '').trim();
              if (updateMessageError) {
                updateMessageError.textContent = rawError
                  ? `Error: ${rawError}`
                  : `Error: ${details.summary}`;
              }
              if (updateMessageAction) {
                updateMessageAction.textContent = details.recommendation
                  ? `Recommended action: ${details.recommendation}`
                  : 'Recommended action: Review Update Log for full context, then retry this component.';
              }
            }
          } else if (updating && activeComponent) {
            const pct = typeof activeComponent.percent === 'number' ? Math.max(0, Math.min(100, activeComponent.percent)) : null;
            updateMessage.classList.remove('error');
            updateMessage.classList.add('active');
            updateMessage.textContent = pct !== null
              ? `${activeComponent.displayName || activeComponent.component || 'Updating'} is updating… ${pct}%`
              : `${activeComponent.displayName || activeComponent.component || 'Updating'} is updating…`;
            resetDetails();
          } else if (updating) {
            updateMessage.classList.remove('error');
            updateMessage.classList.add('active');
            updateMessage.textContent = 'Updating components…';
            resetDetails();
          } else {
            updateMessage.textContent = '';
            updateMessage.classList.remove('active', 'error');
            resetDetails();
          }
        } else {
          resetDetails();
        }
      },

      updateServerStatus: function(serverId, status) {
        const card = document.querySelector(`[data-server-id="${serverId}"]`);

        const statusBadge = card ? card.querySelector('[data-status]') : null;
        const stormBadge = card ? card.querySelector('[data-storm]') : null;
        const uptimeEl = card ? card.querySelector('[data-status-uptime]') : null;

        const summary = (() => {
          const startedAt = typeof status.startedAt === 'string' ? status.startedAt : '';
          const stoppedAt = typeof status.lastStoppedAt === 'string' ? status.lastStoppedAt : '';
          const uptimeSeconds = Number.isFinite(status.uptimeSeconds) ? status.uptimeSeconds : 0;
          const downtimeSeconds = Number.isFinite(status.downtimeSeconds) ? status.downtimeSeconds : 0;
          const format = (secs) => SDSM.relativeTime.formatDuration(secs);
          const base = {
            className: 'is-stopped',
            label: 'Stopped',
            text: stoppedAt ? `Stopped ${format(downtimeSeconds)} ago` : 'Stopped',
            mode: stoppedAt ? 'downtime' : '',
            prefix: stoppedAt ? 'Stopped' : '',
            suffix: stoppedAt ? 'ago' : '',
            source: stoppedAt,
            seconds: downtimeSeconds
          };

          if (status.lastError) {
            return {
              className: 'is-error',
              label: 'Error',
              text: stoppedAt ? `Crashed ${format(downtimeSeconds)} ago` : 'Crashed',
              mode: stoppedAt ? 'downtime' : '',
              prefix: stoppedAt ? 'Crashed' : '',
              suffix: stoppedAt ? 'ago' : '',
              source: stoppedAt,
              seconds: downtimeSeconds
            };
          }

          if (status.stopping) {
            return {
              className: 'is-stopping',
              label: 'Stopping',
              text: 'Stopping...',
              mode: '',
              prefix: '',
              suffix: '',
              source: '',
              seconds: 0
            };
          }

          if (status.starting) {
            return {
              className: 'is-starting',
              label: 'Starting',
              text: 'Starting...',
              mode: '',
              prefix: '',
              suffix: '',
              source: '',
              seconds: 0
            };
          }

          if (status.running && status.paused) {
            return {
              className: 'is-paused',
              label: 'Paused',
              text: `Paused · Up for ${format(uptimeSeconds)}`,
              mode: startedAt ? 'uptime' : '',
              prefix: 'Paused · Up for',
              suffix: '',
              source: startedAt,
              seconds: uptimeSeconds
            };
          }

          if (status.running) {
            return {
              className: 'is-running',
              label: 'Running',
              text: `Running for ${format(uptimeSeconds)}`,
              mode: startedAt ? 'uptime' : '',
              prefix: 'Running for',
              suffix: '',
              source: startedAt,
              seconds: uptimeSeconds
            };
          }

          if (stoppedAt) {
            return base;
          }

          return {
            className: 'is-stopped',
            label: 'Stopped',
            text: 'Never started',
            mode: '',
            prefix: '',
            suffix: '',
            source: '',
            seconds: 0
          };
        })();

        if (statusBadge) {
          statusBadge.classList.remove('is-running', 'is-stopped', 'is-paused', 'is-starting', 'is-stopping', 'is-error');
          if (summary.className) {
            statusBadge.classList.add(summary.className);
          }
          statusBadge.textContent = summary.label;
          statusBadge.setAttribute('aria-label', `Status: ${summary.label}`);
          statusBadge.setAttribute('title', summary.label);
        }

        if (uptimeEl) {
          if (summary.mode && (summary.source || summary.seconds > 0)) {
            let source = summary.source;
            if (!source && summary.seconds > 0) {
              const offset = Math.max(0, summary.seconds) * 1000;
              source = new Date(Date.now() - offset).toISOString();
            }
            if (source) {
              uptimeEl.dataset.relativeMode = summary.mode;
              uptimeEl.dataset.relativeSource = source;
              if (summary.prefix) {
                uptimeEl.dataset.relativePrefix = summary.prefix;
              } else {
                delete uptimeEl.dataset.relativePrefix;
              }
              if (summary.suffix) {
                uptimeEl.dataset.relativeSuffix = summary.suffix;
              } else {
                delete uptimeEl.dataset.relativeSuffix;
              }
              SDSM.relativeTime.render(uptimeEl);
            } else {
              delete uptimeEl.dataset.relativeMode;
              delete uptimeEl.dataset.relativeSource;
              delete uptimeEl.dataset.relativePrefix;
              delete uptimeEl.dataset.relativeSuffix;
              uptimeEl.textContent = summary.text;
            }
          } else {
            delete uptimeEl.dataset.relativeMode;
            delete uptimeEl.dataset.relativeSource;
            delete uptimeEl.dataset.relativePrefix;
            delete uptimeEl.dataset.relativeSuffix;
            uptimeEl.textContent = summary.text;
          }
        }

        if (stormBadge) {
          const isRunning = !!status.running;
          const isStorming = !!status.storming && isRunning;
          stormBadge.classList.remove('is-storm', 'is-clear');

          if (isRunning && isStorming) {
            stormBadge.style.display = '';
            stormBadge.classList.add('is-storm');
            stormBadge.setAttribute('aria-label', 'Storm');
            stormBadge.setAttribute('title', 'Storm');
            stormBadge.textContent = 'Storm';
          } else {
            stormBadge.style.display = 'none';
          }
        }

        if (card) {
          this.updateUsageBars(card, {
            cpuPercent: status.cpuPercent,
            memoryPercent: status.memoryPercent,
            memoryBytes: status.memoryRSSBytes
          });

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
        }

        const navItem = document.querySelector(`.nav-server-item[data-server-id="${serverId}"]`);
        if (navItem) {
          let navState = 'stopped';
          if (status.lastError) {
            navState = 'error';
          } else if (status.starting || status.stopping) {
            navState = 'starting';
          } else if (status.running && status.paused) {
            navState = 'paused';
          } else if (status.running) {
            navState = 'running';
          }
          navItem.dataset.serverState = navState;
          const badge = navItem.querySelector('[data-server-badge]');
          if (badge) {
            badge.hidden = !(status.running && !status.paused);
          }
        }

        document.dispatchEvent(new CustomEvent('sdsm:server-status', {
          detail: {
            serverId: String(serverId),
            status
          }
        }));
      },

      renderSystemHealthCard: function(stats = {}, options = {}) {
        const override = options && options.card;
        const card = override instanceof Element ? override : document.querySelector('[data-system-health-card]');
        if (!card) {
          return;
        }

        const telemetry = stats && typeof stats === 'object'
          ? (stats.systemTelemetry || stats.system_telemetry || null)
          : null;

        const telemetrySample = stats.telemetrySampleISO || stats.telemetrySample || (telemetry && telemetry.sampled_at);
        let telemetrySampleLabel = '';
        let telemetrySampleDate = null;
        if (telemetrySample) {
          const dt = new Date(telemetrySample);
          if (!Number.isNaN(dt.valueOf())) {
            telemetrySampleDate = dt;
            telemetrySampleLabel = dt.toLocaleTimeString();
          } else if (typeof telemetrySample === 'string') {
            telemetrySampleLabel = telemetrySample;
          }
        }

        const telemetryAge = typeof stats.telemetrySampleAge === 'string'
          ? stats.telemetrySampleAge.trim()
          : '';

        const applyTelemetryMeta = () => {
          const pillEl = card.querySelector('[data-system-telemetry-pill]');
          if (pillEl) {
            Array.from(pillEl.classList || []).forEach(cls => {
              if (cls && cls.startsWith('is-')) {
                pillEl.classList.remove(cls);
              }
            });
            (stats.telemetryStatusClass || '').split(/\s+/).forEach(cls => {
              if (cls) {
                pillEl.classList.add(cls);
              }
            });
            pillEl.textContent = stats.telemetryStatus || 'Telemetry';
          }

          const detailContainer = card.querySelector('[data-system-telemetry-detail]') || card;
          const relativeEl = detailContainer.querySelector('[data-system-telemetry-relative]');
          const timeEl = detailContainer.querySelector('[data-system-telemetry-time]');
          const separatorEl = detailContainer.querySelector('[data-system-telemetry-separator]');

          if (relativeEl) {
            if (telemetrySampleDate) {
              relativeEl.dataset.relativeMode = 'telemetry';
              relativeEl.dataset.relativeSource = telemetrySampleDate.toISOString();
              relativeEl.dataset.relativePrefix = 'Updated';
              relativeEl.dataset.relativeSuffix = 'ago';
              if (SDSM.relativeTime && typeof SDSM.relativeTime.render === 'function') {
                SDSM.relativeTime.render(relativeEl);
              } else {
                const fallbackAge = telemetryAge || '0s';
                relativeEl.textContent = `Updated ${fallbackAge} ago`;
              }
            } else {
              delete relativeEl.dataset.relativeMode;
              delete relativeEl.dataset.relativeSource;
              delete relativeEl.dataset.relativePrefix;
              delete relativeEl.dataset.relativeSuffix;
              relativeEl.textContent = (stats.telemetryStatusDetail || 'Awaiting telemetry data');
            }
          }

          if (timeEl) {
            if (telemetrySampleLabel) {
              timeEl.textContent = telemetrySampleLabel;
              if (telemetrySampleDate) {
                timeEl.setAttribute('datetime', telemetrySampleDate.toISOString());
              } else if (stats.telemetrySampleISO) {
                timeEl.setAttribute('datetime', stats.telemetrySampleISO);
              } else {
                timeEl.removeAttribute('datetime');
              }
              timeEl.hidden = false;
            } else {
              timeEl.textContent = '—';
              timeEl.removeAttribute('datetime');
              timeEl.hidden = true;
            }
          }

          if (separatorEl) {
            separatorEl.hidden = !telemetrySampleLabel;
          }
        };

        const componentAlertsEl = card.querySelector('[data-system-component-alerts]');
        const renderComponentAlerts = (alerts) => {
          if (!componentAlertsEl) {
            return;
          }
          componentAlertsEl.innerHTML = '';
          const items = Array.isArray(alerts) ? alerts : [];
          if (!items.length) {
            const empty = document.createElement('li');
            empty.className = 'ops-alert-empty';
            empty.textContent = 'All components nominal';
            componentAlertsEl.appendChild(empty);
            return;
          }
          items.forEach(item => {
            const li = document.createElement('li');
            li.textContent = item;
            componentAlertsEl.appendChild(li);
          });
        };

        applyTelemetryMeta();
        renderComponentAlerts(stats.componentAlerts);

        let percent = Number(stats.systemHealthPercent);
        if (!Number.isFinite(percent)) {
          const snakePercent = Number(stats.system_health_percent);
          if (Number.isFinite(snakePercent)) {
            percent = snakePercent;
          }
        }
        if (!Number.isFinite(percent) && telemetry && Number.isFinite(Number(telemetry.health_percent))) {
          percent = Number(telemetry.health_percent);
        }

        const fallbackLabel = typeof stats.systemHealth === 'string' ? stats.systemHealth.trim() : '';
        const scoreEl = card.querySelector('[data-system-health-score]');
        const percentLabel = this.formatPercent(percent, 0, '', '%');
        if (scoreEl) {
          scoreEl.textContent = percentLabel || fallbackLabel || '—';
        }

        const pillEl = card.querySelector('[data-system-health-pill]');
        if (pillEl) {
          const pillState = this.healthStateForPercent(percent, {
            telemetryAvailable: !!telemetry,
            fallbackLabel
          });
          pillEl.textContent = pillState.label;
          pillEl.classList.remove('is-healthy', 'is-stable', 'is-warning', 'is-critical', 'is-offline');
          if (pillState.className) {
            pillEl.classList.add(pillState.className);
          }
        }

        const updateText = (selector, value, fallback) => {
          const el = card.querySelector(selector);
          if (!el) return;
          const hasValue = value !== undefined && value !== null && !(typeof value === 'string' && value.trim() === '');
          el.textContent = hasValue ? value : (fallback !== undefined ? fallback : '—');
        };

        const updateBar = (selector, value) => {
          const el = card.querySelector(selector);
          if (!el) return;
          const pct = this.clampPercent(value);
          if (pct === null) {
            el.style.width = '0%';
            el.style.opacity = 0.25;
            return;
          }
          el.style.width = `${pct}%`;
          el.style.opacity = pct > 0 ? 1 : 0.25;
        };

        const updateCount = (selector, value) => {
          const el = card.querySelector(selector);
          if (!el) return;
          const num = Number(value);
          if (Number.isFinite(num)) {
            el.textContent = num.toLocaleString();
          } else {
            el.textContent = '—';
          }
        };

        if (!telemetry) {
          updateText('[data-system-cpu-value]');
          updateText('[data-system-cpu-detail]');
          updateText('[data-system-memory-value]');
          updateText('[data-system-memory-detail]', '');
          updateText('[data-system-disk-value]');
          updateText('[data-system-disk-detail]', '');
          updateText('[data-system-net-in]');
          updateText('[data-system-net-out]');
          updateText('[data-system-load]');
          updateText('[data-system-uptime]');
          updateBar('[data-system-cpu-bar]');
          updateBar('[data-system-memory-bar]');
          updateBar('[data-system-disk-bar]');
          updateCount('[data-system-net-interfaces]');
          updateCount('[data-system-procs]');
          return;
        }

        updateText('[data-system-cpu-value]', this.formatPercent(telemetry.cpu_percent));
        updateText('[data-system-cpu-detail]', '—');
        updateBar('[data-system-cpu-bar]', telemetry.cpu_percent);

        updateText('[data-system-memory-value]', this.formatPercent(telemetry.memory_percent));
        updateText('[data-system-memory-detail]', this.formatUsageDetail(telemetry.memory_used, telemetry.memory_total));
        updateBar('[data-system-memory-bar]', telemetry.memory_percent);

        updateText('[data-system-disk-value]', this.formatPercent(telemetry.disk_percent));
        updateText('[data-system-disk-detail]', this.formatUsageDetail(telemetry.disk_used, telemetry.disk_total));
        updateBar('[data-system-disk-bar]', telemetry.disk_percent);

        updateText('[data-system-net-in]', this.formatRateWithTotal(telemetry.network_in_bps, telemetry.network_in_bytes));
        updateText('[data-system-net-out]', this.formatRateWithTotal(telemetry.network_out_bps, telemetry.network_out_bytes));
        updateText('[data-system-load]', this.formatLoadAverage(telemetry.load1, telemetry.load5, telemetry.load15));

        const uptimeLabel = this.formatUptime(telemetry.uptime_seconds);
        updateText('[data-system-uptime]', uptimeLabel ? `Up ${uptimeLabel}` : '—');

        updateCount('[data-system-net-interfaces]', telemetry.network_interfaces);
        updateCount('[data-system-procs]', telemetry.process_count);
      },

      updateStats: function(stats) {
        stats = stats || {};
        SDSM.state.lastStats = stats;

        const setText = (id, value) => {
          if (value === undefined || value === null) {
            return;
          }
          const el = typeof id === 'string' ? document.getElementById(id) : id;
          if (el) {
            el.textContent = value;
          }
        };

        const clampPercent = (value) => {
          const num = typeof value === 'number' ? value : parseFloat(value);
          if (!Number.isFinite(num)) {
            return 0;
          }
          return Math.max(0, Math.min(100, num));
        };

        const setBarPercent = (id, value) => {
          const el = document.getElementById(id);
          if (el) {
            el.style.width = `${clampPercent(value)}%`;
          }
        };

        const updateTimeElement = (id, dateValue) => {
          const el = document.getElementById(id);
          if (!el || !dateValue) {
            return;
          }
          const dt = new Date(dateValue);
          if (!Number.isNaN(dt.valueOf())) {
            el.textContent = dt.toLocaleTimeString();
            el.setAttribute('datetime', dt.toISOString());
          } else {
            el.textContent = dateValue;
            el.removeAttribute('datetime');
          }
        };

        const setStatusPill = (el, text, className) => {
          if (!el) {
            return;
          }
          if (text !== undefined) {
            el.textContent = text || 'Telemetry';
          }
          Array.from(el.classList || []).forEach(cls => {
            if (cls && cls.startsWith('is-')) {
              el.classList.remove(cls);
            }
          });
          (className || '').split(/\s+/).forEach(cls => {
            if (cls) {
              el.classList.add(cls);
            }
          });
        };

        const renderAlertList = (container, alerts) => {
          if (!container) {
            return;
          }
          container.innerHTML = '';
          const items = Array.isArray(alerts) ? alerts : [];
          if (!items.length) {
            const empty = document.createElement('li');
            empty.className = 'ops-alert-empty';
            empty.textContent = 'All components nominal';
            container.appendChild(empty);
            return;
          }
          items.forEach(item => {
            const li = document.createElement('li');
            li.textContent = item;
            container.appendChild(li);
          });
        };

        if (stats.totalServers !== undefined) {
          setText('total-servers', stats.totalServers);

          const hTotal = document.getElementById('header-total-servers');
          if (hTotal) hTotal.textContent = stats.totalServers;

          const hSuffix = document.getElementById('header-total-servers-suffix');
          if (hSuffix) hSuffix.textContent = stats.totalServers === 1 ? '' : 's';
        }

        if (stats.activeServers !== undefined) {
          setText('active-servers', stats.activeServers);

          const hActive = document.getElementById('header-active-servers');
          if (hActive) hActive.textContent = stats.activeServers;
        }

        if (stats.totalPlayers !== undefined) {
          setText('total-players', stats.totalPlayers);

          const hPlayers = document.getElementById('header-total-players');
          if (hPlayers) hPlayers.textContent = stats.totalPlayers;
        }

        if (stats.startableServers !== undefined) {
          setText('startable-servers', stats.startableServers);
        }

        if (stats.pendingUpdates !== undefined) {
          setText('cluster-pending-updates', stats.pendingUpdates);
        }

        if (stats.componentsHealthy !== undefined && stats.componentsTotal !== undefined) {
          setText('cluster-pending-meta', `${stats.componentsHealthy}/${stats.componentsTotal} healthy`);
        }

        if (stats.systemHealthLabel || stats.systemHealthPercent !== undefined) {
          setText('cluster-health-value', stats.systemHealthLabel || this.formatPercent(stats.systemHealthPercent));
        }
        if (stats.systemHealthPercent !== undefined) {
          setBarPercent('cluster-health-bar', stats.systemHealthPercent);
        }

        if (stats.cpuPercentLabel || stats.cpuPercent !== undefined) {
          setText('cluster-cpu-value', stats.cpuPercentLabel || this.formatPercent(stats.cpuPercent));
        }
        if (stats.cpuPercent !== undefined) {
          setBarPercent('cluster-cpu-bar', stats.cpuPercent);
        }

        if (stats.memoryPercentLabel || stats.memoryPercent !== undefined) {
          setText('cluster-memory-value', stats.memoryPercentLabel || this.formatPercent(stats.memoryPercent));
        }
        if (stats.memoryPercent !== undefined) {
          setBarPercent('cluster-memory-bar', stats.memoryPercent);
        }
        if (stats.memoryDetail || (stats.memoryUsed !== undefined && stats.memoryTotal !== undefined)) {
          setText('cluster-memory-detail', stats.memoryDetail || this.formatUsageDetail(stats.memoryUsed, stats.memoryTotal));
        }

        if (stats.diskPercentLabel || stats.diskPercent !== undefined) {
          setText('cluster-disk-value', stats.diskPercentLabel || this.formatPercent(stats.diskPercent));
        }
        if (stats.diskPercent !== undefined) {
          setBarPercent('cluster-disk-bar', stats.diskPercent);
        }
        if (stats.diskDetail || (stats.diskUsed !== undefined && stats.diskTotal !== undefined)) {
          setText('cluster-disk-detail', stats.diskDetail || this.formatUsageDetail(stats.diskUsed, stats.diskTotal));
        }

        const loadMetaEl = document.getElementById('cluster-load-label');
        if (loadMetaEl) {
          loadMetaEl.textContent = stats.loadAverageLabel ? `Load ${stats.loadAverageLabel}` : '—';
        }
        const loadInlineEl = document.getElementById('cluster-load-inline');
        if (loadInlineEl) {
          loadInlineEl.textContent = stats.loadAverageLabel || '—';
        }
        const uptimeLabelEl = document.getElementById('cluster-uptime-label');
        if (uptimeLabelEl) {
          uptimeLabelEl.textContent = stats.uptimeLabel || '—';
        }

        const telemetryAge = typeof stats.telemetrySampleAge === 'string' ? stats.telemetrySampleAge.trim() : '';
        const sample = stats.telemetrySampleISO || stats.telemetrySample;
        let sampleLabel = '';
        let sampleDate = null;
        if (sample) {
          const dt = new Date(sample);
          if (!Number.isNaN(dt.valueOf())) {
            sampleDate = dt;
            sampleLabel = dt.toLocaleTimeString();
          } else if (typeof sample === 'string') {
            sampleLabel = sample;
          }
        }

        const inlineAge = document.getElementById('cluster-telemetry-age-inline');
        if (inlineAge) {
          inlineAge.textContent = telemetryAge || '—';
        }
        const inlineStamp = document.getElementById('cluster-telemetry-stamp-inline');
        if (inlineStamp) {
          inlineStamp.textContent = sampleLabel || 'Awaiting data';
        }

        const metaAge = document.getElementById('cluster-telemetry-age');
        if (metaAge) {
          metaAge.textContent = telemetryAge ? `Sampled ${telemetryAge} ago` : 'Awaiting telemetry';
        }
        const metaTime = document.getElementById('cluster-telemetry-time');
        if (metaTime) {
          if (sampleLabel) {
            metaTime.textContent = sampleLabel;
            if (sampleDate) {
              metaTime.setAttribute('datetime', sampleDate.toISOString());
            } else if (stats.telemetrySampleISO) {
              metaTime.setAttribute('datetime', stats.telemetrySampleISO);
            } else {
              metaTime.removeAttribute('datetime');
            }
          } else {
            metaTime.textContent = '—';
            metaTime.removeAttribute('datetime');
          }
        }

        setStatusPill(document.getElementById('cluster-telemetry-pill'), stats.telemetryStatus, stats.telemetryStatusClass);
        const telemetryDetail = document.getElementById('cluster-telemetry-detail');
        if (telemetryDetail) {
          telemetryDetail.textContent = stats.telemetryStatusDetail || 'Awaiting telemetry data';
        }

        renderAlertList(document.getElementById('cluster-component-alerts'), stats.componentAlerts);

        const refresh = document.getElementById('last-refresh');
        if (refresh) {
          refresh.textContent = new Date().toLocaleTimeString();
        }

        updateTimeElement('cluster-stats-updated', stats.lastUpdatedISO || stats.lastUpdated);

        this.renderSystemHealthCard(stats);

        document.dispatchEvent(new CustomEvent('sdsm:stats-update', { detail: stats }));
      },

      bindServerCardNavigation: function(root) {
        const cards = (root || document).querySelectorAll('.server-card');
        cards.forEach(card => {
          if (card.dataset.navigationBound === 'true') return;
          card.dataset.navigationBound = 'true';

          card.addEventListener('click', e => {
            if (e.target.closest('[data-stop-navigation="true"]')) return;
            const tilesCard = card.closest('.server-tiles-card');
            if (tilesCard && tilesCard.classList.contains('is-selecting')) {
              return;
            }
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

    cards: {
      registry: Object.create(null),
      assetMap: {
        'dashboard-manager': [{ type: 'js', path: '/static/js/cards/dashboard_manager.js' }],
        'dashboard-users': [{ type: 'js', path: '/static/js/cards/dashboard_users.js' }],
        'dashboard-system-health': [{ type: 'js', path: '/static/js/cards/dashboard_system_health.js' }],
        'dashboard-stats': [{ type: 'js', path: '/static/js/cards/dashboard_stats.js' }]
      },
      loading: Object.create(null),
      styleRegistry: Object.create(null),
      instances: new WeakMap(),
      htmxBound: false,
      lazyObserver: null,
      pendingRefresh: Object.create(null),

      init: function(scope) {
        const root = scope instanceof Element || scope instanceof Document ? scope : document;
        this.scan(root);
      },

      bindHtmxHooks: function() {
        if (this.htmxBound || typeof htmx === 'undefined') {
          return;
        }
        this.htmxBound = true;
        const handleBeforeSwap = (event) => {
          const target = event?.target;
          if (target instanceof Element) {
            this.teardown(target);
          }
        };
        const handleBeforeCleanup = (event) => {
          const target = event?.detail?.target || event?.target;
          if (target instanceof Element) {
            this.teardown(target);
          }
        };
        document.body.addEventListener('htmx:beforeSwap', handleBeforeSwap);
        document.body.addEventListener('htmx:beforeCleanupElement', handleBeforeCleanup);
      },

      scan: function(scope) {
        if (!scope) {
          return;
        }
        const nodes = this.collect(scope);
        nodes.forEach((node) => this.mount(node));
      },

      collect: function(scope) {
        const nodes = [];
        const add = (el) => {
          if (el && nodes.indexOf(el) === -1) {
            nodes.push(el);
          }
        };
        const root = scope instanceof Element ? scope : null;
        if (root && typeof root.matches === 'function' && root.matches('[data-card-id]')) {
          add(root);
        }
        const searchRoot = root || (scope instanceof Document ? scope : (scope && scope.querySelectorAll ? scope : document));
        if (searchRoot && typeof searchRoot.querySelectorAll === 'function') {
          searchRoot.querySelectorAll('[data-card-id]').forEach(add);
        }
        return nodes;
      },

      isLazyCard: function(card) {
        if (!(card instanceof Element)) {
          return false;
        }
        if (typeof card.hasAttribute === 'function' && card.hasAttribute('data-card-lazy')) {
          const raw = (card.getAttribute('data-card-lazy') || '').trim().toLowerCase();
          return raw === '' || raw === 'true' || raw === '1';
        }
        if (card.dataset && Object.prototype.hasOwnProperty.call(card.dataset, 'cardLazy')) {
          const raw = (card.dataset.cardLazy || '').trim().toLowerCase();
          return raw === '' || raw === 'true' || raw === '1';
        }
        return false;
      },

      isLazyReady: function(card) {
        if (!this.isLazyCard(card)) {
          return true;
        }
        const marker = (card.dataset && card.dataset.cardLazyReady) || card.getAttribute('data-card-lazy-ready') || '';
        return marker.toString().toLowerCase() === 'true';
      },

      markLazyReady: function(card) {
        if (!(card instanceof Element)) {
          return;
        }
        if (card.dataset) {
          card.dataset.cardLazyReady = 'true';
          delete card.dataset.cardLazyPending;
        } else {
          card.setAttribute('data-card-lazy-ready', 'true');
          card.removeAttribute('data-card-lazy-pending');
        }
      },

      ensureLazyObserver: function() {
        if (this.lazyObserver) {
          return this.lazyObserver;
        }
        if (typeof window === 'undefined' || typeof window.IntersectionObserver === 'undefined') {
          return null;
        }
        this.lazyObserver = new IntersectionObserver((entries) => this.handleLazyIntersections(entries), {
          root: null,
          rootMargin: '200px 0px',
          threshold: 0.05
        });
        return this.lazyObserver;
      },

      handleLazyIntersections: function(entries) {
        if (!Array.isArray(entries)) {
          return;
        }
        const schedule = (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function')
          ? window.requestAnimationFrame.bind(window)
          : (fn) => setTimeout(fn, 0);
        entries.forEach((entry) => {
          if (!entry || !entry.isIntersecting) {
            return;
          }
          const card = entry.target;
          if (!(card instanceof Element)) {
            return;
          }
          this.unobserveLazy(card);
          this.markLazyReady(card);
          const cardId = (card.dataset && card.dataset.cardId) || card.getAttribute('data-card-id') || '';
          schedule(() => {
            this.mount(card);
            if (cardId) {
              this.flushPendingRefresh(cardId);
            }
          });
        });
      },

      deferLazyMount: function(card) {
        if (!(card instanceof Element) || !this.isLazyCard(card)) {
          return false;
        }
        if (this.isLazyReady(card)) {
          return false;
        }
        if (card.dataset && card.dataset.cardLazyPending === 'true') {
          return true;
        }
        const observer = this.ensureLazyObserver();
        if (!observer) {
          this.markLazyReady(card);
          return false;
        }
        if (card.dataset) {
          card.dataset.cardLazyPending = 'true';
        } else {
          card.setAttribute('data-card-lazy-pending', 'true');
        }
        observer.observe(card);
        return true;
      },

      unobserveLazy: function(card) {
        if (!(card instanceof Element)) {
          return;
        }
        if (this.lazyObserver) {
          try {
            this.lazyObserver.unobserve(card);
          } catch (_) {}
        }
        if (card.dataset) {
          delete card.dataset.cardLazyPending;
        } else if (typeof card.removeAttribute === 'function') {
          card.removeAttribute('data-card-lazy-pending');
        }
      },

      captureDeclaredAssets: function(cardId, card) {
        if (!cardId || !(card instanceof Element)) {
          return;
        }
        const markerAttr = card.dataset ? card.dataset.cardAssetsParsed : card.getAttribute('data-card-assets-parsed');
        if (markerAttr === 'true') {
          return;
        }
        const rawValue = (card.dataset && card.dataset.cardAssets) || card.getAttribute('data-card-assets');
        if (!rawValue) {
          if (card.dataset) {
            card.dataset.cardAssetsParsed = 'true';
          } else {
            card.setAttribute('data-card-assets-parsed', 'true');
          }
          return;
        }
        const parsed = this.parseAssetAttribute(rawValue);
        if (parsed.length) {
          this.registerAsset(cardId, parsed);
        }
        if (card.dataset) {
          card.dataset.cardAssetsParsed = 'true';
        } else {
          card.setAttribute('data-card-assets-parsed', 'true');
        }
      },

      parseAssetAttribute: function(value) {
        const raw = (value || '').trim();
        if (!raw) {
          return [];
        }
        const firstChar = raw.charAt(0);
        if ((firstChar === '[' || firstChar === '{') && raw.length >= 2) {
          try {
            const parsed = JSON.parse(raw);
            if (Array.isArray(parsed)) {
              return parsed;
            }
            return parsed ? [parsed] : [];
          } catch (err) {
            console.warn('SDSM.cards: failed to parse data-card-assets attribute', err);
            return [];
          }
        }
        return raw.split(/[,\s]+/).map((entry) => entry.trim()).filter(Boolean);
      },

      normalizeAssetDescriptor: function(entry) {
        if (!entry) {
          return null;
        }
        if (typeof entry === 'string') {
          return this.normalizeAssetDescriptor({ path: entry });
        }
        if (typeof entry !== 'object') {
          return null;
        }
        const path = (entry.path || entry.src || '').trim();
        if (!path) {
          return null;
        }
        const descriptor = {
          path,
          type: this.detectAssetType(path, entry.type)
        };
        if (descriptor.type === 'js') {
          const typeHint = (entry.type || '').toString().toLowerCase();
          descriptor.module = entry.module === true || typeHint === 'module';
          if (entry.async === false) {
            descriptor.async = false;
          } else if (entry.async === true) {
            descriptor.async = true;
          }
          if (entry.defer === true) {
            descriptor.defer = true;
          }
        } else if (entry.media) {
          descriptor.media = String(entry.media);
        }
        return descriptor;
      },

      detectAssetType: function(path, hint) {
        const normalized = (hint || '').toString().trim().toLowerCase();
        if (normalized === 'css' || normalized === 'style') {
          return 'css';
        }
        if (normalized === 'module' || normalized === 'js' || normalized === 'script') {
          return 'js';
        }
        if (/\.css(?:[?#].*)?$/i.test(path)) {
          return 'css';
        }
        return 'js';
      },

      ensureStyles: function(cardId) {
        if (!cardId) {
          return;
        }
        const assets = this.assetMap[cardId];
        if (!Array.isArray(assets) || assets.length === 0) {
          return;
        }
        assets.forEach((asset) => {
          if (asset && asset.type === 'css') {
            this.loadStyleAsset(asset);
          }
        });
      },

      loadStyleAsset: function(asset) {
        if (!asset || !asset.path) {
          return;
        }
        if (this.styleRegistry[asset.path]) {
          return;
        }
        this.styleRegistry[asset.path] = true;
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = this.withCacheBuster(asset.path);
        if (asset.media) {
          link.media = asset.media;
        }
        link.dataset.cardStyle = asset.path;
        document.head.appendChild(link);
      },

      mount: function(card) {
        if (!(card instanceof Element)) {
          return;
        }
        if (this.deferLazyMount(card)) {
          return;
        }
        const cardId = (card.dataset && card.dataset.cardId) || card.getAttribute('data-card-id') || '';
        if (!cardId) {
          return;
        }
        this.captureDeclaredAssets(cardId, card);
        this.ensureStyles(cardId);
        const existing = this.instances.get(card);
        if (existing && existing.id === cardId) {
          return;
        }
        this.unmount(card);

        const baseCleanup = this.bindLifecycle(card);
        const module = this.registry[cardId];
        if (!module) {
          baseCleanup && this.instances.set(card, { id: cardId, cleanup: baseCleanup, module: null });
          this.ensureModule(cardId, card, baseCleanup);
          return;
        }

        let moduleCleanup = null;
        try {
          if (typeof module.mount === 'function') {
            moduleCleanup = module.mount(card, { id: cardId, refresh: this.refresh.bind(this) }) || null;
          } else if (typeof module === 'function') {
            moduleCleanup = module(card, { id: cardId, refresh: this.refresh.bind(this) }) || null;
          }
        } catch (err) {
          console.error(`Card module "${cardId}" failed during mount:`, err);
        }

        const cleanup = () => {
          if (typeof moduleCleanup === 'function') {
            try {
              moduleCleanup(card);
            } catch (err) {
              console.error(`Card module "${cardId}" failed during cleanup:`, err);
            }
          } else if (module && typeof module.destroy === 'function') {
            try {
              module.destroy(card);
            } catch (err) {
              console.error(`Card module "${cardId}" destroy error:`, err);
            }
          }
          if (typeof baseCleanup === 'function') {
            baseCleanup();
          }
        };

        this.instances.set(card, { id: cardId, cleanup, module });
      },

      ensureModule: function(cardId, card, baseCleanup) {
        const assets = this.assetMap[cardId];
        const scriptAssets = Array.isArray(assets) ? assets.filter((asset) => asset && asset.type === 'js') : [];
        if (scriptAssets.length === 0) {
          return;
        }
        this.loadScriptsSequential(scriptAssets).then(() => {
          this.mount(card);
        }).catch((err) => {
          console.error(`Failed to load card module "${cardId}":`, err);
          if (typeof baseCleanup === 'function') {
            baseCleanup();
          }
        });
      },

      bindLifecycle: function(card) {
        if (!(card instanceof Element)) {
          return null;
        }
        const setBusy = (state) => {
          if (state) {
            card.setAttribute('aria-busy', 'true');
            card.dataset.cardLoading = 'true';
          } else {
            card.removeAttribute('aria-busy');
            delete card.dataset.cardLoading;
          }
        };
        const handleStart = (event) => {
          if (event?.target === card) {
            setBusy(true);
          }
        };
        const handleEnd = (event) => {
          if (event?.target === card) {
            setBusy(false);
          }
        };
        card.addEventListener('htmx:beforeRequest', handleStart);
        card.addEventListener('htmx:afterRequest', handleEnd);
        card.addEventListener('htmx:responseError', handleEnd);
        card.addEventListener('htmx:sendError', handleEnd);
        return () => {
          card.removeEventListener('htmx:beforeRequest', handleStart);
          card.removeEventListener('htmx:afterRequest', handleEnd);
          card.removeEventListener('htmx:responseError', handleEnd);
          card.removeEventListener('htmx:sendError', handleEnd);
          setBusy(false);
        };
      },

      unmount: function(card) {
        this.unobserveLazy(card);
        const instance = this.instances.get(card);
        if (!instance) {
          return;
        }
        if (typeof instance.cleanup === 'function') {
          try {
            instance.cleanup(card);
          } catch (err) {
            console.error(`Card module "${instance.id}" cleanup error:`, err);
          }
        }
        this.instances.delete(card);
      },

      teardown: function(scope) {
        const nodes = this.collect(scope);
        nodes.forEach((node) => this.unmount(node));
      },

      define: function(cardId, module) {
        if (!cardId || !module) {
          return;
        }
        this.registry[cardId] = module;
        const nodes = document.querySelectorAll(`[data-card-id="${cardId}"]`);
        nodes.forEach((node) => this.mount(node));
      },

      registerAsset: function(cardId, asset) {
        if (!cardId || !asset) {
          return;
        }
        const entries = Array.isArray(asset) ? asset : [asset];
        const bucket = this.assetMap[cardId] || [];
        entries.forEach((entry) => {
          const descriptor = this.normalizeAssetDescriptor(entry);
          if (!descriptor) {
            return;
          }
          const exists = bucket.some((item) => item && item.type === descriptor.type && item.path === descriptor.path);
          if (!exists) {
            bucket.push(descriptor);
          }
        });
        if (bucket.length) {
          this.assetMap[cardId] = bucket;
        }
      },

      loadScriptsSequential: function(assets) {
        if (!Array.isArray(assets) || assets.length === 0) {
          return Promise.resolve();
        }
        return assets.reduce((promise, asset) => promise.then(() => this.loadScriptAsset(asset)), Promise.resolve());
      },

      loadScriptAsset: function(asset) {
        if (!asset || !asset.path) {
          return Promise.resolve();
        }
        const key = `${asset.module ? 'module:' : ''}${asset.path}`;
        if (this.loading[key]) {
          return this.loading[key];
        }
        this.loading[key] = new Promise((resolve, reject) => {
          const script = document.createElement('script');
          script.src = this.withCacheBuster(asset.path);
          script.async = false;
          script.defer = false;
          if (asset.async === true) {
            script.async = true;
          }
          if (asset.defer === true) {
            script.defer = true;
          }
          if (asset.module) {
            script.type = 'module';
          }
          script.dataset.cardScript = key;
          script.onload = () => resolve();
          script.onerror = (err) => reject(err || new Error(`Failed to load ${asset.path}`));
          document.head.appendChild(script);
        });
        return this.loading[key];
      },

      withCacheBuster: function(url) {
        const buildTime = (window.SDSM && window.SDSM.state && window.SDSM.state.buildTime) || document.body?.dataset?.buildTime || '';
        if (!buildTime) {
          return url;
        }
        const separator = url.includes('?') ? '&' : '?';
        return `${url}${separator}v=${encodeURIComponent(buildTime)}`;
      },

      refresh: function(cardId) {
        if (!cardId) {
          return;
        }
        const nodes = document.querySelectorAll(`[data-card-id="${cardId}"]`);
        if (!nodes.length) {
          this.dispatchRefreshEvent(cardId);
          return;
        }
        const ready = Array.from(nodes).some((node) => !this.isLazyCard(node) || this.isLazyReady(node));
        if (!ready) {
          this.queueLazyRefresh(cardId);
          return;
        }
        this.dispatchRefreshEvent(cardId);
      },

      queueLazyRefresh: function(cardId) {
        if (!cardId) {
          return;
        }
        if (!this.pendingRefresh) {
          this.pendingRefresh = Object.create(null);
        }
        this.pendingRefresh[cardId] = true;
      },

      flushPendingRefresh: function(cardId) {
        if (!cardId || !this.pendingRefresh || !this.pendingRefresh[cardId]) {
          return;
        }
        delete this.pendingRefresh[cardId];
        this.dispatchRefreshEvent(cardId);
      },

      dispatchRefreshEvent: function(cardId) {
        if (!cardId) {
          return;
        }
        const target = document.body || document;
        target.dispatchEvent(new CustomEvent('sdsm:card-refresh', {
          detail: { cardId }
        }));
      }
    },

    dashboard: {
      root: null,
      managerCard: null,
      userCard: null,
      refreshInterval: null,

      init(scope) {
        const container = this.findContainer(scope);
        if (!container) {
          if (!scope || scope === document) {
            this.destroy();
          }
          return;
        }
        this.root = container;
        this.managerCard = container.querySelector('[data-manager-card]');
        this.userCard = container.querySelector('[data-user-card]');
        this.refresh();
        this.startPolling();
      },

      findContainer(scope) {
        const searchRoot = scope instanceof Element ? scope : document;
        if (searchRoot && searchRoot instanceof Element && searchRoot.hasAttribute('data-dashboard-root')) {
          return searchRoot;
        }
        return searchRoot && searchRoot.querySelector ? searchRoot.querySelector('[data-dashboard-root]') : null;
      },

      startPolling() {
        if (this.refreshInterval) {
          clearInterval(this.refreshInterval);
        }
        if (!this.managerCard) {
          this.refreshInterval = null;
          return;
        }
        this.refreshInterval = window.setInterval(() => this.refreshManagerCard(), 30000);
      },

      destroy() {
        if (this.refreshInterval) {
          clearInterval(this.refreshInterval);
          this.refreshInterval = null;
        }
        this.root = null;
        this.managerCard = null;
        this.userCard = null;
      },

      refresh() {
        this.refreshManagerCard();
        this.refreshUserStats();
      },

      refreshManagerCard() {
        if (!this.managerCard) {
          return;
        }
        this.dispatchCardRefresh('dashboard-manager');
      },

      refreshUserStats() {
        if (!this.userCard) {
          return;
        }
        if ((SDSM.state.role || '').toLowerCase() !== 'admin') {
          return;
        }
        this.dispatchCardRefresh('dashboard-users');
      },

      dispatchCardRefresh(cardId) {
        if (SDSM.cards && typeof SDSM.cards.refresh === 'function') {
          SDSM.cards.refresh(cardId);
          return;
        }
        if (!cardId) {
          return;
        }
        const target = document.body || document;
        target.dispatchEvent(new CustomEvent('sdsm:card-refresh', {
          detail: { cardId }
        }));
      },

      setText(el, value) {
        if (!el) {
          return;
        }
        el.textContent = value;
      },

      toggleHidden(el, hidden) {
        if (!el) {
          return;
        }
        if (hidden) {
          el.classList.add('hidden');
        } else {
          el.classList.remove('hidden');
        }
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
      },

      syncTlsFields: function(forcedState) {
        const toggle = document.getElementById('tls_enabled');
        const enabled = typeof forcedState === 'boolean' ? forcedState : (toggle ? toggle.checked : false);
        const nodes = document.querySelectorAll('[data-requires-tls]');
        nodes.forEach((node) => {
          node.disabled = !enabled;
          if (!enabled) {
            node.setAttribute('aria-disabled', 'true');
          } else {
            node.removeAttribute('aria-disabled');
          }
        });
      }
    },

    // Token reference helpers
    tokens: {
      registry: {
        'server-defaults': [
          {
            token: '{{server_name}}',
            label: 'Server name',
            description: 'The display name configured for the server.'
          },
          {
            token: '{{event}}',
            label: 'Event keyword',
            description: 'Lifecycle keyword such as started, stopping, stopped, or restarting.'
          },
          {
            token: '{{detail}}',
            label: 'Detail text',
            description: 'Additional context about the lifecycle event. May be empty.'
          },
          {
            token: '{{timestamp}}',
            label: 'Timestamp',
            description: 'When the event occurred (UTC).'
          }
        ],
        'deploy-events': [
          {
            token: '{{component}}',
            label: 'Component',
            description: 'Component being deployed (SteamCMD, Release, Beta, etc.).'
          },
          {
            token: '{{status}}',
            label: 'Status',
            description: 'Deploy status keyword such as started, completed, error, or skipped.'
          },
          {
            token: '{{duration}}',
            label: 'Duration',
            description: 'Human-friendly elapsed time for the deploy action.'
          },
          {
            token: '{{errors}}',
            label: 'Errors',
            description: 'Condensed error summary (present only for error cases).'
          },
          {
            token: '{{timestamp}}',
            label: 'Timestamp',
            description: 'When the deploy event occurred.'
          }
        ]
      },

      normalizeKey: function(context) {
        return (context || '').toString().trim().toLowerCase();
      },

      getDefinitions: function(context) {
        const key = this.normalizeKey(context);
        return key ? (this.registry[key] || []) : [];
      },

      buildReferenceBody: function(defs, introText) {
        const wrapper = document.createElement('div');
        wrapper.className = 'token-reference';

        const intro = document.createElement('p');
        intro.className = 'token-reference-intro';
        intro.textContent = introText || 'Click a token to copy it to your clipboard.';
        wrapper.appendChild(intro);

        const grid = document.createElement('div');
        grid.className = 'token-reference-grid';

        defs.forEach((def) => {
          const row = document.createElement('div');
          row.className = 'token-reference-row';

          const chip = document.createElement('button');
          chip.type = 'button';
          chip.className = 'token-chip';
          chip.textContent = def.token;
          chip.setAttribute('data-token-value', def.token);
          chip.setAttribute('aria-label', `Copy ${def.token}`);

          const details = document.createElement('div');
          details.className = 'token-reference-details';

          const label = document.createElement('div');
          label.className = 'token-reference-label';
          label.textContent = def.label || def.token;
          details.appendChild(label);

          if (def.description) {
            const desc = document.createElement('p');
            desc.className = 'token-reference-desc';
            desc.textContent = def.description;
            details.appendChild(desc);
          }

          row.appendChild(chip);
          row.appendChild(details);
          grid.appendChild(row);
        });

        wrapper.appendChild(grid);
        return wrapper;
      },

      copyToken: function(token) {
        if (!token) {
          return Promise.resolve();
        }
        if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
          return navigator.clipboard.writeText(token);
        }
        return new Promise((resolve, reject) => {
          try {
            const textarea = document.createElement('textarea');
            textarea.value = token;
            textarea.setAttribute('readonly', '');
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.select();
            const success = document.execCommand('copy');
            document.body.removeChild(textarea);
            if (!success) {
              reject(new Error('Copy command was rejected.'));
            } else {
              resolve();
            }
          } catch (err) {
            reject(err);
          }
        });
      },

      showReference: function(context, options = {}) {
        const defs = this.getDefinitions(context || 'server-defaults');
        if (!defs.length) {
          console.warn('No token definitions found for context:', context);
          return;
        }

        const body = this.buildReferenceBody(defs, options.intro);
        const title = options.title || 'Supported Tokens';
        const modalFn = SDSM.modal && typeof SDSM.modal.info === 'function' ? SDSM.modal.info : null;

        if (!modalFn) {
          const fallbackList = defs.map((def) => `${def.token} — ${def.description || def.label || ''}`).join('\n');
          window.alert(`${title}\n\n${fallbackList}`.trim());
          return;
        }

        modalFn({
          title,
          body,
          buttonText: options.buttonText || 'Done',
          onRender: ({ close }) => {
            body.querySelectorAll('[data-token-value]').forEach((btn) => {
              btn.addEventListener('click', () => {
                const value = btn.getAttribute('data-token-value');
                if (!value) return;
                this.copyToken(value).then(() => {
                  if (window.showToast) {
                    window.showToast('Copied', `${value} copied to clipboard.`, 'success');
                  }
                  close();
                }).catch((err) => {
                  console.error('Failed to copy token:', err);
                  if (window.showToast) {
                    window.showToast('Copy failed', 'Unable to copy token to clipboard.', 'danger');
                  }
                });
              });
            });
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

    // Collapsible sections with persisted state
    collapses: {
      storagePrefix: 'sdsm-collapse-',

      init: function(root) {
        const scope = root instanceof Element ? root : document;
        const sections = Array.from(scope.querySelectorAll('[data-collapse-id]'));
        if (scope instanceof Element && scope.matches('[data-collapse-id]')) {
          sections.push(scope);
        }
        sections.forEach((details) => {
          if (!(details instanceof Element) || details.tagName !== 'DETAILS') {
            return;
          }
          this.restore(details);
          if (details.dataset.collapseBound === 'true') {
            this.update(details);
            return;
          }
          details.dataset.collapseBound = 'true';
          details.addEventListener('toggle', () => {
            this.persist(details);
            this.update(details);
          });
          this.update(details);
        });
      },

      key: function(details) {
        const id = details?.dataset?.collapseId;
        return id ? `${this.storagePrefix}${id}` : null;
      },

      restore: function(details) {
        const key = this.key(details);
        if (!key) return;
        try {
          const stored = localStorage.getItem(key);
          if (stored === 'open') {
            details.open = true;
          } else if (stored === 'closed') {
            details.open = false;
          }
        } catch (_) {
          // ignore storage errors
        }
      },

      persist: function(details) {
        const key = this.key(details);
        if (!key) return;
        try {
          localStorage.setItem(key, details.open ? 'open' : 'closed');
        } catch (_) {
          // ignore storage errors
        }
      },

      update: function(details) {
        const summary = details.querySelector('summary');
        if (!summary) return;
        summary.setAttribute('aria-expanded', details.open ? 'true' : 'false');

        const icon = summary.querySelector('[data-collapse-icon]');
        if (icon) {
          icon.classList.toggle('is-open', details.open);
        }
      }
    },

    // Generic log viewer – works for both manager and server logs cards.
    // Each card element must have:
    //   data-log-viewer           – marks the card as a log viewer instance
    //   data-log-base-url="..."   – API prefix, e.g. "/api/manager" or "/api/servers/1"
    // Inside the card use data-* attributes for child elements:
    //   data-log-tabs, data-log-empty, data-log-view, data-log-status,
    //   data-log-refresh, data-log-download, data-log-clear
    logViewer: {
      tailInterval: 4000,
      maxBuffer: 500000,
      instances: new WeakMap(),

      init: function(root) {
        const scope = root instanceof Element ? root : document;
        const cards = scope.querySelectorAll('[data-log-viewer]');
        cards.forEach((card) => {
          if (card && card.isConnected) {
            this.mount(card);
          }
        });
      },

      mount: function(card) {
        if (!card || this.instances.has(card)) {
          return;
        }
        const baseUrl = card.getAttribute('data-log-base-url') || '';
        const baseUrlKey = baseUrl ? String(baseUrl).replace(/[^a-zA-Z0-9_-]+/g, '_') : 'default';
        const cardId = card.id || `log-viewer-${baseUrlKey}`;
        const panelId = `${cardId}-panel`;
        const state = {
          card,
          baseUrl,
          storageKey: `sdsm-log-active-${baseUrlKey}`,
          tabPrefix: `${cardId}-tab`,
          panelId,
          tabs: card.querySelector('[data-log-tabs]'),
          empty: card.querySelector('[data-log-empty]'),
          view: card.querySelector('[data-log-view]'),
          status: card.querySelector('[data-log-status]'),
          refresh: card.querySelector('[data-log-refresh]'),
          download: card.querySelector('[data-log-download]'),
          clear: card.querySelector('[data-log-clear]'),
          activeLog: null,
          offset: -1,
          pollTimer: null,
          tailController: null,
          autoScroll: true,
          buffer: '',
          loadingList: false,
          hadError: false,
          lastSize: 0,
        };

        if (state.view) {
          if (!state.view.id) {
            state.view.id = panelId;
          } else {
            state.panelId = state.view.id;
          }
          state.view.setAttribute('role', 'tabpanel');
        }

        this.instances.set(card, state);
        this.bind(card, state);
        this.fetchList(card, state);
      },

      bind: function(card, state) {
        const self = this;
        if (state.tabs) {
          state.tabs.addEventListener('click', (event) => {
            const tab = event.target.closest('[data-log-file]');
            if (!tab) return;
            const file = tab.dataset.logFile;
            if (file) {
              self.activate(card, state, file);
            }
          });
          state.tabs.addEventListener('keydown', (event) => {
            const current = event.target instanceof Element ? event.target.closest('[data-log-file]') : null;
            if (!current) return;

            const tabs = Array.from(state.tabs.querySelectorAll('[data-log-file]'));
            if (!tabs.length) return;

            const currentIndex = tabs.indexOf(current);
            if (currentIndex < 0) return;

            let nextIndex = -1;
            switch (event.key) {
              case 'Enter':
              case ' ':
                event.preventDefault();
                {
                  const file = current.dataset.logFile;
                  if (file) {
                    self.activate(card, state, file);
                  }
                }
                return;
              case 'ArrowLeft':
              case 'ArrowUp':
                nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
                break;
              case 'ArrowRight':
              case 'ArrowDown':
                nextIndex = (currentIndex + 1) % tabs.length;
                break;
              case 'Home':
                nextIndex = 0;
                break;
              case 'End':
                nextIndex = tabs.length - 1;
                break;
              default:
                return;
            }

            event.preventDefault();
            const nextTab = tabs[nextIndex];
            if (!nextTab) return;
            nextTab.focus();

            const file = nextTab.dataset.logFile;
            if (file) {
              self.activate(card, state, file);
            }
          });
        }
        if (state.refresh) {
          state.refresh.addEventListener('click', () => {
            self.fetchList(card, state, { force: true });
          });
        }
        if (state.download) {
          state.download.addEventListener('click', () => {
            if (!state.activeLog) {
              if (window.showToast) window.showToast('Info', 'Please select a log file to download.', 'info');
              return;
            }
            window.location.href = `${state.baseUrl}/log/download?name=${encodeURIComponent(state.activeLog)}`;
          });
        }
        if (state.clear) {
          state.clear.addEventListener('click', () => {
            if (!state.activeLog) {
              if (window.showToast) window.showToast('Info', 'Select a log file to clear.', 'info');
              return;
            }

            const doClear = () => {
              fetch(`${state.baseUrl}/log/clear`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'HX-Request': 'true' },
                credentials: 'same-origin',
                body: JSON.stringify({ name: state.activeLog })
              })
              .then((resp) => {
                if (!resp.ok) throw new Error('Unable to clear log.');
                state.offset = -1;
                state.buffer = '';
                self.fetchList(card, state, { force: true });
              })
              .catch((err) => {
                if (window.showToast) window.showToast('Logs', err.message || 'Unable to clear log.', 'danger');
              });
            };

            const hasConfirmTemplate = !!document.getElementById('tpl-modal-confirm');
            const canUseModalConfirm = Boolean(window.SDSM && SDSM.modal && typeof SDSM.modal.confirm === 'function' && hasConfirmTemplate);

            if (canUseModalConfirm) {
              SDSM.modal.confirm({
                title: 'Clear Log File',
                body: `Are you sure you want to clear \"${state.activeLog}\"?`,
                confirmText: 'Clear Log',
                cancelText: 'Cancel',
                danger: true,
              }).then((confirmed) => {
                if (confirmed) {
                  doClear();
                }
              }).catch(() => {
                if (window.confirm(`Are you sure you want to clear \"${state.activeLog}\"?`)) {
                  doClear();
                }
              });
              return;
            }

            if (window.confirm(`Are you sure you want to clear \"${state.activeLog}\"?`)) {
              doClear();
            }
          });
        }
        if (state.view) {
          state.view.addEventListener('scroll', () => {
            if (!state.view) return;
            const distance = state.view.scrollHeight - state.view.clientHeight - state.view.scrollTop;
            state.autoScroll = distance < 24;
          });
        }
      },

      destroy: function(card) {
        const state = this.instances.get(card);
        if (!state) return;
        this.clearTimers(state);
        this.instances.delete(card);
      },

      clearTimers: function(state) {
        if (state.pollTimer) {
          clearTimeout(state.pollTimer);
          state.pollTimer = null;
        }
        if (state.tailController) {
          state.tailController.abort();
          state.tailController = null;
        }
      },

      fetchList: function(card, state, options = {}) {
        if (!card.isConnected) {
          this.destroy(card);
          return;
        }
        if (state.loadingList && !options.force) {
          return;
        }
        state.loadingList = true;
        if (state.empty) {
          state.empty.textContent = 'Loading log list\u2026';
          state.empty.classList.remove('hidden');
        }
        fetch(`${state.baseUrl}/logs`, {
          method: 'GET',
          headers: { Accept: 'application/json', 'HX-Request': 'true' },
          credentials: 'same-origin'
        })
        .then((resp) => {
          if (resp.status === 403) {
            throw new Error('Access denied.');
          }
          if (!resp.ok) {
            throw new Error('Unable to load log list.');
          }
          return resp.json();
        })
        .then((payload) => {
          if (!card.isConnected) {
            this.destroy(card);
            return;
          }
          const files = Array.isArray(payload.files) ? payload.files : [];
          this.renderTabs(card, state, files);
        })
        .catch((err) => {
          this.showListError(state, err);
        })
        .finally(() => {
          state.loadingList = false;
        });
      },

      showListError: function(state, error) {
        if (state.empty) {
          state.empty.textContent = error && error.message ? error.message : 'Unable to load logs.';
          state.empty.classList.remove('hidden');
        }
        if (state.view && !state.view.textContent.trim()) {
          state.view.textContent = 'Select a log to stream its output.';
        }
      },

      renderTabs: function(card, state, files) {
        if (!state.tabs) return;
        state.tabs.innerHTML = '';
        if (!files.length) {
          if (state.empty) {
            state.empty.textContent = 'No log files detected.';
            state.empty.classList.remove('hidden');
          }
          this.clearTimers(state);
          state.activeLog = null;
          state.buffer = '';
          if (state.view) {
            state.view.textContent = 'No log data available.';
          }
          this.updateStatus(state);
          return;
        }
        if (state.empty) {
          state.empty.classList.add('hidden');
        }
        const fragment = document.createDocumentFragment();
        files.forEach((file, index) => {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'tab';
          btn.textContent = file;
          btn.dataset.logFile = file;
          btn.setAttribute('role', 'tab');
          btn.setAttribute('aria-selected', 'false');
          btn.id = `${state.tabPrefix}-${index + 1}`;
          if (state.panelId) {
            btn.setAttribute('aria-controls', state.panelId);
          }
          fragment.appendChild(btn);
        });
        state.tabs.appendChild(fragment);
        const desired = this.pickDefaultLog(state, files);
        this.activate(card, state, desired, { force: true });
      },

      pickDefaultLog: function(state, files) {
        if (!Array.isArray(files) || files.length === 0) {
          return null;
        }

        const normalized = files.map((f) => String(f || ''));

        if (state.activeLog && normalized.includes(state.activeLog)) {
          return state.activeLog;
        }

        let remembered = '';
        try {
          remembered = String(localStorage.getItem(state.storageKey) || '').trim();
        } catch (_) {
          remembered = '';
        }
        if (remembered && normalized.includes(remembered)) {
          return remembered;
        }

        const score = (name) => {
          const n = String(name || '').toLowerCase();
          if (!n) return -1;
          if (n === 'sdsm.log') return 100;
          if (n.startsWith('sdsm') && n.endsWith('.log')) return 95;
          if (n.includes('sdsm') && n.endsWith('.log')) return 90;
          if (n.includes('manager') && n.endsWith('.log')) return 80;
          if (n.endsWith('.log')) return 70;
          if (n.includes('update')) return 60;
          return 10;
        };

        let best = normalized[0];
        let bestScore = score(best);
        for (let i = 1; i < normalized.length; i++) {
          const candidate = normalized[i];
          const s = score(candidate);
          if (s > bestScore) {
            best = candidate;
            bestScore = s;
          }
        }
        return best;
      },

      activate: function(card, state, logFile, options = {}) {
        if (!logFile || (!options.force && state.activeLog === logFile)) {
          return;
        }
        state.activeLog = logFile;
        state.offset = -1;
        state.buffer = '';
        state.hadError = false;
        try {
          localStorage.setItem(state.storageKey, logFile);
        } catch (_) {
          // ignore storage errors
        }
        this.clearTimers(state);
        this.updateTabs(state);
        if (state.view) {
          state.view.textContent = 'Connecting to log\u2026';
        }
        this.updateStatus(state, { pending: true });
        this.pollTail(card, state);
      },

      updateTabs: function(state) {
        if (!state.tabs) return;
        const buttons = state.tabs.querySelectorAll('[data-log-file]');
        let activeButton = null;
        buttons.forEach((btn) => {
          const isActive = btn.dataset.logFile === state.activeLog;
          btn.classList.toggle('active', isActive);
          btn.setAttribute('aria-selected', isActive ? 'true' : 'false');
          btn.setAttribute('tabindex', isActive ? '0' : '-1');
          if (isActive) {
            activeButton = btn;
          }
        });

        if (state.view) {
          if (activeButton && activeButton.id) {
            state.view.setAttribute('aria-labelledby', activeButton.id);
          } else {
            state.view.removeAttribute('aria-labelledby');
          }
        }
      },

      pollTail: function(card, state) {
        if (!state.activeLog) {
          return;
        }
        if (!card.isConnected) {
          this.destroy(card);
          return;
        }
        if (state.tailController) {
          state.tailController.abort();
        }
        const controller = new AbortController();
        state.tailController = controller;
        const params = new URLSearchParams({
          name: state.activeLog,
          offset: String(typeof state.offset === 'number' ? state.offset : -1),
          back: '8192',
          max: '65536'
        });
        fetch(`${state.baseUrl}/log/tail?${params.toString()}`, {
          method: 'GET',
          headers: { Accept: 'application/json', 'HX-Request': 'true' },
          credentials: 'same-origin',
          signal: controller.signal
        })
        .then((resp) => {
          if (!resp.ok) {
            throw new Error('Unable to tail log.');
          }
          return resp.json();
        })
        .then((payload) => {
          this.handleTailSuccess(state, payload);
          this.scheduleNext(card, state, this.tailInterval);
        })
        .catch((err) => {
          if (err.name === 'AbortError') {
            return;
          }
          this.handleTailError(state, err);
          this.scheduleNext(card, state, this.tailInterval * 1.5);
        })
        .finally(() => {
          state.tailController = null;
        });
      },

      scheduleNext: function(card, state, delay) {
        if (!card.isConnected || !state.activeLog) {
          this.destroy(card);
          return;
        }
        const wait = Number.isFinite(delay) ? Math.max(1200, delay) : this.tailInterval;
        state.pollTimer = window.setTimeout(() => {
          state.pollTimer = null;
          this.pollTail(card, state);
        }, wait);
      },

      handleTailSuccess: function(state, payload) {
        if (!payload) return;
        if (payload.reset) {
          state.buffer = '';
        }
        if (typeof payload.offset === 'number') {
          state.offset = payload.offset;
        }
        if (typeof payload.size === 'number') {
          state.lastSize = payload.size;
        }
        const chunk = typeof payload.data === 'string' ? payload.data : '';
        this.appendChunk(state, chunk, payload.reset);
        this.updateStatus(state);
        state.hadError = false;
      },

      handleTailError: function(state, error) {
        if (!state.hadError && window.showToast) {
          window.showToast('Logs', error && error.message ? error.message : 'Unable to tail log.', 'danger');
        }
        state.hadError = true;
        if (state.status) {
          state.status.textContent = error && error.message ? error.message : 'Unable to tail log.';
        }
      },

      appendChunk: function(state, chunk, reset) {
        if (!state.view) return;
        if (reset) {
          state.buffer = '';
        }
        if (chunk) {
          const normalized = chunk.replace(/\r\n/g, '\n');
          state.buffer = (state.buffer || '') + normalized;
          if (state.buffer.length > this.maxBuffer) {
            state.buffer = state.buffer.slice(state.buffer.length - this.maxBuffer);
          }
        }
        if (!state.buffer) {
          state.view.textContent = 'Waiting for log data\u2026';
          return;
        }
        const stick = state.autoScroll || (state.view.scrollHeight - state.view.clientHeight - state.view.scrollTop < 24);
        state.view.textContent = state.buffer;
        if (stick) {
          state.view.scrollTop = state.view.scrollHeight;
        }
      },

      updateStatus: function(state, opts = {}) {
        if (!state.status) return;
        if (!state.activeLog) {
          state.status.textContent = '';
          return;
        }
        if (opts.pending) {
          state.status.textContent = `Connecting to ${state.activeLog}\u2026`;
          return;
        }
        const sizeLabel = typeof state.lastSize === 'number' && state.lastSize >= 0
          ? (SDSM.ui && typeof SDSM.ui.formatBytes === 'function' ? SDSM.ui.formatBytes(state.lastSize) : `${state.lastSize} bytes`)
          : 'Size unknown';
        const timestamp = new Date().toLocaleTimeString();
        state.status.textContent = `Streaming ${state.activeLog} \u00b7 ${sizeLabel} \u00b7 Updated ${timestamp}`;
      }
    },

    // Backward compat alias
    managerLogs: {
      init: function(root) { SDSM.logViewer.init(root); }
    },

    // Frame Management
    frame: {
      pageTitles: {
        '/dashboard': 'Dashboard',
        '/manager': 'Manager Settings',
        '/help/tokens': 'Chat Tokens',
      },

      managerSubmenu: {
        container: null,
        contentArea: null,
        sections: [],
        activeId: null,
        currentPath: '',
        scrollTarget: null,
        scrollHandler: null,
        scrollRaf: null,

        init() {
          this.container = document.querySelector('[data-manager-submenu]');
          this.contentArea = document.getElementById('content-area');
          if (this.container) {
            this.container.setAttribute('role', 'menu');
          }
        },

        isManagerPath(path) {
          return path === '/manager';
        },

        refreshForPath(path) {
          this.currentPath = path || '';
          if (!this.container) {
            return;
          }
          if (!this.isManagerPath(path)) {
            this.hide();
            this.clear();
            return;
          }
          this.contentArea = document.getElementById('content-area');
          this.show();
          this.build();
        },

        handleContentSwap(target) {
          if (!this.container || !target || target.id !== 'content-area') {
            return;
          }
          this.contentArea = target;
          if (this.isManagerPath(this.currentPath || window.location.pathname)) {
            this.show();
            this.build(target);
          }
        },

        clear() {
          this.unbindScrollTracking();
          if (!this.container) {
            return;
          }
          this.sections = [];
          this.activeId = null;
          this.container.innerHTML = '';
        },

        show() {
          if (!this.container) {
            return;
          }
          this.container.hidden = false;
          this.container.classList.add('active');
        },

        hide() {
          if (!this.container) {
            return;
          }
          this.container.classList.remove('active');
          this.container.hidden = true;
        },

        build(root) {
          if (!this.container) {
            return;
          }
          const scope = root || this.contentArea || document.getElementById('content-area');
          if (!scope) {
            this.clear();
            this.hide();
            return;
          }
          this.contentArea = scope;
          const nodes = Array.from(scope.querySelectorAll('[data-manager-section]'));
          if (!nodes.length) {
            this.clear();
            this.hide();
            return;
          }
          this.sections = nodes.map((node, index) => {
            if (!node.id) {
              node.id = `manager-section-${index + 1}`;
            }
            const title = (node.dataset.sectionTitle || this.extractHeading(node) || `Section ${index + 1}`).trim();
            return { id: node.id, title, node };
          });
          this.renderButtons();
          this.bindScrollTracking();
          if (this.sections[0]) {
            this.updateActiveFromScroll();
          }
        },

        extractHeading(node) {
          const heading = node.querySelector('[data-section-label], .card-title, h2, h3, h4, summary');
          return heading ? heading.textContent : '';
        },

        renderButtons() {
          if (!this.container) {
            return;
          }
          const fragment = document.createDocumentFragment();
          this.container.innerHTML = '';
          this.sections.forEach((section) => {
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'nav-submenu-item';
            btn.textContent = section.title;
            btn.dataset.sectionId = section.id;
            btn.setAttribute('role', 'menuitem');
            btn.setAttribute('aria-current', 'false');
            btn.addEventListener('click', () => this.scrollToSection(section.id));
            section.button = btn;
            fragment.appendChild(btn);
          });
          this.container.appendChild(fragment);
        },

        prefersContentScroll() {
          const area = this.contentArea || document.getElementById('content-area');
          return !!(area && area.scrollHeight - area.clientHeight > 12);
        },

        scrollToSection(sectionId) {
          const target = document.getElementById(sectionId);
          if (!target) {
            return;
          }
          const area = this.contentArea || document.getElementById('content-area');
          if (area && this.prefersContentScroll()) {
            const containerRect = area.getBoundingClientRect();
            const targetRect = target.getBoundingClientRect();
            const offset = targetRect.top - containerRect.top + area.scrollTop - 12;
            area.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
          } else {
            const top = target.getBoundingClientRect().top + window.pageYOffset - 80;
            window.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
          }
          this.setActive(sectionId);
        },

        setActive(sectionId) {
          this.activeId = sectionId;
          this.sections.forEach((section) => {
            if (section.button) {
              const active = section.id === sectionId;
              section.button.classList.toggle('is-active', active);
              section.button.setAttribute('aria-current', active ? 'true' : 'false');
            }
          });
        },

        getScrollTarget() {
          const area = this.contentArea || document.getElementById('content-area');
          if (area && this.prefersContentScroll()) {
            return area;
          }
          return window;
        },

        bindScrollTracking() {
          this.unbindScrollTracking();
          if (!this.sections.length) {
            return;
          }

          const target = this.getScrollTarget();
          this.scrollTarget = target;
          this.scrollHandler = () => {
            if (this.scrollRaf) {
              return;
            }
            this.scrollRaf = window.requestAnimationFrame(() => {
              this.scrollRaf = null;
              this.updateActiveFromScroll();
            });
          };

          target.addEventListener('scroll', this.scrollHandler, { passive: true });
          window.addEventListener('resize', this.scrollHandler);
        },

        unbindScrollTracking() {
          if (this.scrollTarget && this.scrollHandler) {
            this.scrollTarget.removeEventListener('scroll', this.scrollHandler);
          }
          if (this.scrollHandler) {
            window.removeEventListener('resize', this.scrollHandler);
          }
          if (this.scrollRaf) {
            window.cancelAnimationFrame(this.scrollRaf);
            this.scrollRaf = null;
          }
          this.scrollTarget = null;
          this.scrollHandler = null;
        },

        updateActiveFromScroll() {
          if (!this.sections.length) {
            return;
          }

          const target = this.getScrollTarget();
          const marker = 140;
          let best = this.sections[0];

          if (target === window) {
            const scrollY = window.scrollY || window.pageYOffset || 0;
            this.sections.forEach((section) => {
              const top = section.node.getBoundingClientRect().top + scrollY;
              if (top <= scrollY + marker) {
                best = section;
              }
            });
          } else {
            const area = target;
            const areaRect = area.getBoundingClientRect();
            this.sections.forEach((section) => {
              const rect = section.node.getBoundingClientRect();
              const relTop = rect.top - areaRect.top + area.scrollTop;
              if (relTop <= area.scrollTop + marker) {
                best = section;
              }
            });
          }

          if (best && best.id !== this.activeId) {
            this.setActive(best.id);
          }
        }
      },

      serverSubmenu: {
        containers: {},
        contentArea: null,
        sections: [],
        activeId: null,
        currentPath: '',

        init() {
          this.contentArea = document.getElementById('content-area');
          document.querySelectorAll('[data-server-submenu]').forEach((el) => {
            const id = el.getAttribute('data-server-submenu');
            el.setAttribute('role', 'menu');
            this.containers[id] = el;
          });
        },

        serverIdFromPath(path) {
          const m = /^\/server\/(\d+)$/.exec(path);
          return m ? m[1] : null;
        },

        refreshForPath(path) {
          this.currentPath = path || '';
          const activeId = this.serverIdFromPath(path);
          // Hide all server submenus
          Object.keys(this.containers).forEach((id) => {
            if (id !== activeId) {
              this.hideContainer(this.containers[id]);
              this.clearContainer(this.containers[id]);
            }
          });
          if (!activeId || !this.containers[activeId]) {
            this.sections = [];
            this.activeId = null;
            return;
          }
          this.contentArea = document.getElementById('content-area');
          this.showContainer(this.containers[activeId]);
          this.build(activeId);
        },

        handleContentSwap(target) {
          if (!target || target.id !== 'content-area') return;
          this.contentArea = target;
          const activeId = this.serverIdFromPath(this.currentPath || window.location.pathname);
          if (activeId && this.containers[activeId]) {
            this.showContainer(this.containers[activeId]);
            this.build(activeId, target);
          }
        },

        clearContainer(container) {
          if (!container) return;
          container.innerHTML = '';
        },

        showContainer(container) {
          if (!container) return;
          container.hidden = false;
          container.classList.add('active');
        },

        hideContainer(container) {
          if (!container) return;
          container.classList.remove('active');
          container.hidden = true;
        },

        build(serverId, root) {
          const container = this.containers[serverId];
          if (!container) return;
          const scope = root || this.contentArea || document.getElementById('content-area');
          if (!scope) {
            this.clearContainer(container);
            this.hideContainer(container);
            return;
          }
          this.contentArea = scope;
          const nodes = Array.from(scope.querySelectorAll('[data-server-section]'));
          if (!nodes.length) {
            this.clearContainer(container);
            this.hideContainer(container);
            return;
          }
          this.sections = nodes.map((node, index) => {
            if (!node.id) {
              node.id = `server-section-${index + 1}`;
            }
            const title = (node.dataset.sectionTitle || this.extractHeading(node) || `Section ${index + 1}`).trim();
            return { id: node.id, title, node };
          });
          this.renderButtons(container);
          if (this.sections[0]) {
            this.setActive(this.sections[0].id);
          }
        },

        extractHeading(node) {
          const heading = node.querySelector('[data-section-label], .card-title, h2, h3, h4, summary');
          return heading ? heading.textContent : '';
        },

        renderButtons(container) {
          if (!container) return;
          const fragment = document.createDocumentFragment();
          container.innerHTML = '';
          this.sections.forEach((section) => {
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'nav-submenu-item';
            btn.textContent = section.title;
            btn.dataset.sectionId = section.id;
            btn.setAttribute('role', 'menuitem');
            btn.setAttribute('aria-current', 'false');
            btn.addEventListener('click', () => this.scrollToSection(section.id));
            section.button = btn;
            fragment.appendChild(btn);
          });
          container.appendChild(fragment);
        },

        prefersContentScroll() {
          const area = this.contentArea || document.getElementById('content-area');
          return !!(area && area.scrollHeight - area.clientHeight > 12);
        },

        scrollToSection(sectionId) {
          const target = document.getElementById(sectionId);
          if (!target) return;
          const area = this.contentArea || document.getElementById('content-area');
          if (area && this.prefersContentScroll()) {
            const containerRect = area.getBoundingClientRect();
            const targetRect = target.getBoundingClientRect();
            const offset = targetRect.top - containerRect.top + area.scrollTop - 12;
            area.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
          } else {
            const top = target.getBoundingClientRect().top + window.pageYOffset - 80;
            window.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
          }
          this.setActive(sectionId);
        },

        setActive(sectionId) {
          this.activeId = sectionId;
          this.sections.forEach((section) => {
            if (section.button) {
              const active = section.id === sectionId;
              section.button.classList.toggle('is-active', active);
              section.button.setAttribute('aria-current', active ? 'true' : 'false');
            }
          });
        }
      },

      sidebar: {
        storageKey: 'sdsm:sidebar-collapsed',
        toggleSelector: '[data-sidebar-toggle]',
        collapsed: false,
        toggleBtn: null,
        swapHandlerBound: false,

        init() {
          const root = document.body;
          this.toggleBtn = document.querySelector(this.toggleSelector);
          this.collapsed = this.readStoredState();
          this.apply(this.collapsed, { skipPersist: true, skipTooltipRefresh: true });

          if (this.toggleBtn && !this.toggleBtn.dataset.sidebarToggleBound) {
            this.toggleBtn.dataset.sidebarToggleBound = 'true';
            this.toggleBtn.addEventListener('click', (event) => {
              event.preventDefault();
              this.apply(!this.collapsed);
            });
          }

          if (!this.swapHandlerBound) {
            this.swapHandlerBound = true;
            document.addEventListener('htmx:afterSwap', (evt) => {
              const target = evt?.target instanceof Element ? evt.target : null;
              if (!target) {
                return;
              }
              if (target.closest('.sidebar') || target.closest('.nav-server-list')) {
                this.updateNavTooltips();
              }
            });
          }

          // Mobile menu toggle
          const mobileMenuBtn = document.getElementById('mobile-menu-btn');
          const sidebarOverlay = document.getElementById('sidebar-overlay');
          if (mobileMenuBtn && !mobileMenuBtn.dataset.bound) {
            mobileMenuBtn.dataset.bound = 'true';
            mobileMenuBtn.addEventListener('click', () => this.toggleMobileMenu());
          }
          if (sidebarOverlay && !sidebarOverlay.dataset.bound) {
            sidebarOverlay.dataset.bound = 'true';
            sidebarOverlay.addEventListener('click', () => this.closeMobileMenu());
          }

          // On desktop, restore collapsed state; on mobile, ensure sidebar-open is cleared
          const handleResize = () => {
            if (!root) {
              return;
            }
            if (window.matchMedia('(max-width: 1024px)').matches) {
              root.classList.remove('sidebar-collapsed');
            } else {
              this.closeMobileMenu();
              if (this.collapsed) {
                root.classList.add('sidebar-collapsed');
              }
            }
          };
          handleResize();
          window.addEventListener('resize', handleResize);

          this.updateNavTooltips();
        },

        readStoredState() {
          try {
            return localStorage.getItem(this.storageKey) === '1';
          } catch (_) {
            return false;
          }
        },

        apply(collapsed, options = {}) {
          const root = document.body;
          this.collapsed = !!collapsed;
          if (root) {
            root.classList.toggle('sidebar-collapsed', this.collapsed);
          }
          if (this.toggleBtn) {
            this.toggleBtn.setAttribute('aria-pressed', this.collapsed ? 'true' : 'false');
            this.toggleBtn.setAttribute('aria-label', this.collapsed ? 'Expand sidebar' : 'Collapse sidebar');
          }
          if (!options.skipPersist) {
            try {
              localStorage.setItem(this.storageKey, this.collapsed ? '1' : '0');
            } catch (err) {
              console.warn('Unable to persist sidebar preference', err);
            }
          }
          if (!options.skipTooltipRefresh) {
            this.updateNavTooltips();
          }
        },

        updateNavTooltips() {
          const root = document.body;
          const collapsed = Boolean(this.collapsed && root && root.classList.contains('sidebar-collapsed'));
          const items = document.querySelectorAll('.sidebar .nav-item');
          items.forEach((item) => {
            if (!item.dataset.navLabel) {
              const labelEl = item.querySelector('.nav-item-text') || item.querySelector('span:last-child');
              const labelText = labelEl ? labelEl.textContent.trim() : item.textContent.trim();
              if (labelText) {
                item.dataset.navLabel = labelText;
              }
            }
            if (collapsed) {
              if (item.dataset.navLabel) {
                item.setAttribute('title', item.dataset.navLabel);
              }
            } else if (item.dataset.navLabel && item.getAttribute('title') === item.dataset.navLabel) {
              item.removeAttribute('title');
            }
          });
        },

        toggleMobileMenu() {
          const root = document.body;
          if (!root) return;
          const isOpen = root.classList.contains('sidebar-open');
          if (isOpen) {
            this.closeMobileMenu();
          } else {
            root.classList.add('sidebar-open');
            const btn = document.getElementById('mobile-menu-btn');
            if (btn) btn.setAttribute('aria-expanded', 'true');
          }
        },

        closeMobileMenu() {
          const root = document.body;
          if (!root) return;
          root.classList.remove('sidebar-open');
          const btn = document.getElementById('mobile-menu-btn');
          if (btn) btn.setAttribute('aria-expanded', 'false');
        }
      },

      updateTitle: (path) => {
        const titleEl = document.getElementById('page-title');
        if (!titleEl) return;

        if (/^\/server\/(\d+)$/.test(path)) {
          let label = '';
          document.querySelectorAll('.nav-item[data-target]').forEach((item) => {
            if (!label && item.dataset.target === path) {
              const textEl = item.querySelector('.nav-item-text');
              label = (textEl ? textEl.textContent : item.textContent || '').trim();
            }
          });
          if (!label) {
            label = 'Server ' + path.replace(/^\/server\//, '');
          }
          titleEl.textContent = label;
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
        SDSM.frame.managerSubmenu.init();
        SDSM.frame.serverSubmenu.init();
        SDSM.frame.sidebar.init();

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
          SDSM.frame.managerSubmenu.refreshForPath(targetPath);
          SDSM.frame.serverSubmenu.refreshForPath(targetPath);
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
              SDSM.frame.managerSubmenu.handleContentSwap(evt.target);
              SDSM.frame.serverSubmenu.handleContentSwap(evt.target);
            }
          });
        }

        window.addEventListener('popstate', () => syncNavState(window.location.pathname));

        document.body.addEventListener('click', (evt) => {
          const target = evt.target instanceof Element ? evt.target.closest('.nav-item') : null;
          if (target && target.dataset.target) {
            SDSM.frame.updateActiveNav(target.dataset.target);
            SDSM.frame.managerSubmenu.refreshForPath(target.dataset.target);
            SDSM.frame.serverSubmenu.refreshForPath(target.dataset.target);
            // Close the mobile drawer after navigation
            if (window.matchMedia('(max-width: 1024px)').matches) {
              SDSM.frame.sidebar.closeMobileMenu();
            }
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
      const body = document.body;
      if (body && body.dataset) {
        this.state.role = body.dataset.role || this.state.role;
        this.state.username = body.dataset.username || this.state.username;
        this.state.page = body.dataset.page || this.state.page;
        this.state.buildTime = body.dataset.buildTime || this.state.buildTime;
      }

      // Initialize keyboard shortcuts
      this.keyboard.init();

      // Kick off relative time updates for uptime badges
      this.relativeTime.init();

      this.dashboard.init();
      this.ui.startStatsPoll();
      if (this.cards) {
        this.cards.bindHtmxHooks();
        this.cards.init(document);
      }
      if (this.cardSort) {
        this.cardSort.init(document);
      }

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
        const toggleHxIndicator = (event, hidden) => {
          const selector = event?.detail?.elt?.getAttribute('hx-indicator');
          if (!selector) {
            return;
          }
          try {
            document.querySelectorAll(selector).forEach((indicator) => {
              indicator.classList.toggle('hidden', hidden);
            });
          } catch (err) {
            console.warn('Invalid hx-indicator selector', selector, err);
          }
        };

        document.body.addEventListener('htmx:beforeRequest', (ev) => toggleHxIndicator(ev, false));
        document.body.addEventListener('htmx:afterRequest', (ev) => toggleHxIndicator(ev, true));
        document.body.addEventListener('htmx:responseError', (ev) => toggleHxIndicator(ev, true));
        document.body.addEventListener('htmx:sendError', (ev) => toggleHxIndicator(ev, true));

        document.addEventListener('htmx:afterSwap', (e) => {
          const swapRoot = e.target instanceof Element ? e.target : null;

          if (swapRoot && typeof initServerCreationPage === 'function') {
            initServerCreationPage(swapRoot);
          }

          if (swapRoot && swapRoot.closest('#server-grid')) {
            this.ui.bindServerCardNavigation(swapRoot.closest('#server-grid'));
          }

          // When #content-area is swapped, update the frame title from the
          // new content's data-page-title attribute if present.
          if (swapRoot && swapRoot.id === 'content-area') {
            const titleEl = document.getElementById('page-title');
            if (titleEl) {
              const wrapper = swapRoot.querySelector('[data-page-title]');
              if (wrapper && wrapper.dataset.pageTitle) {
                titleEl.textContent = wrapper.dataset.pageTitle;
              }
            }
            SDSM.frame.managerSubmenu.handleContentSwap(swapRoot);
            SDSM.frame.serverSubmenu.handleContentSwap(swapRoot);
          }

          if (swapRoot) {
            this.relativeTime.refresh(swapRoot);
            this.collapses.init(swapRoot);
            this.logViewer.init(swapRoot);
            if (this.cards) {
              this.cards.init(swapRoot);
            }
            // cardSort self-initialises via htmx:load in card-sort.js.
          } else {
            this.relativeTime.refresh(e.target);
          }

          if (swapRoot && (swapRoot.id === 'manager-settings-form' || swapRoot.querySelector('#manager-settings-form'))) {
            this.forms.syncTlsFields();
          }

          if (swapRoot) {
            this.dashboard.init(swapRoot);
          }
        });
      }

      document.body.addEventListener('click', (event) => {
        const targetEl = event.target instanceof Element ? event.target : null;
        if (!targetEl) return;

        const modalConfirmTrigger = targetEl.closest('[data-modal-confirm]');
        if (modalConfirmTrigger) {
          if (typeof event.button === 'number' && event.button !== 0) return;
          if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
          if (modalConfirmTrigger.disabled) return;

          // Allow one bypassed click through after confirmation so HTMX can proceed.
          if (modalConfirmTrigger.dataset.modalConfirmBypass === '1') {
            delete modalConfirmTrigger.dataset.modalConfirmBypass;
            return;
          }

          const message = modalConfirmTrigger.getAttribute('data-modal-message') ||
            modalConfirmTrigger.getAttribute('data-confirm-message') ||
            'Are you sure?';
          const title = modalConfirmTrigger.getAttribute('data-modal-title') || 'Confirm Action';
          const confirmText = modalConfirmTrigger.getAttribute('data-modal-confirm-text') || 'Confirm';
          const cancelText = modalConfirmTrigger.getAttribute('data-modal-cancel-text') || 'Cancel';
          const dangerRaw = (modalConfirmTrigger.getAttribute('data-modal-danger') || '').toLowerCase();
          const danger = dangerRaw === '1' || dangerRaw === 'true' || dangerRaw === 'yes';

          const continueAction = () => {
            modalConfirmTrigger.dataset.modalConfirmBypass = '1';
            if (window.htmx && typeof window.htmx.trigger === 'function') {
              window.htmx.trigger(modalConfirmTrigger, 'click');
            } else {
              modalConfirmTrigger.click();
            }
          };

          const nativeFallback = () => {
            if (window.confirm(message)) {
              continueAction();
            }
          };

          const hasConfirmTemplate = !!document.getElementById('tpl-modal-confirm');
          const canUseModalConfirm = Boolean(window.SDSM && SDSM.modal && typeof SDSM.modal.confirm === 'function' && hasConfirmTemplate);

          event.preventDefault();
          event.stopPropagation();

          if (!canUseModalConfirm) {
            nativeFallback();
            return;
          }

          SDSM.modal.confirm({
            title,
            body: message,
            confirmText,
            cancelText,
            danger,
          }).then((confirmed) => {
            if (confirmed) {
              continueAction();
            }
          }).catch((err) => {
            console.error('Modal confirmation failed:', err);
            nativeFallback();
          });
          return;
        }

        const pathPickerBtn = targetEl.closest('[data-path-picker]');
        if (pathPickerBtn) {
          if (pathPickerBtn.disabled) return;
          event.preventDefault();
          const targetId = pathPickerBtn.getAttribute('data-path-picker');
          if (!targetId) return;
          const input = document.getElementById(targetId);
          if (!input) return;

          const label = pathPickerBtn.getAttribute('data-picker-label') || 'Filesystem Path';
          const placeholder = pathPickerBtn.getAttribute('data-picker-placeholder') || input.placeholder || '';
          const currentValue = input.value || '';
          const defaultValue = currentValue || placeholder || '';
          const pickerDescription = pathPickerBtn.getAttribute('data-picker-description') || label;
          const pickerMode = pathPickerBtn.getAttribute('data-picker-mode') || 'directory';
          const confirmLabel = pathPickerBtn.getAttribute('data-picker-confirm') || 'Use Path';

          const applyValue = (value) => {
            if (value === null || typeof value === 'undefined') return;
            const finalValue = value.toString().trim();
            if (!finalValue) return;
            input.value = finalValue;
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new Event('change', { bubbles: true }));
          };

          const fallbackPrompt = () => {
            const response = window.prompt(label, defaultValue);
            if (response !== null) {
              applyValue(response);
            }
          };

          const hasPromptTemplate = !!document.getElementById('tpl-modal-prompt');
          const canUseModalPrompt = Boolean(window.SDSM && SDSM.modal && typeof SDSM.modal.prompt === 'function' && hasPromptTemplate);
          const hasPathPickerTemplate = !!document.getElementById('tpl-modal-path-picker');
          const canUsePathPicker = Boolean(window.SDSM && SDSM.pathPicker && typeof SDSM.pathPicker.open === 'function' && hasPathPickerTemplate);

          if (canUsePathPicker) {
            SDSM.pathPicker.open({
              title: pathPickerBtn.getAttribute('data-picker-title') || 'Select Path',
              description: pickerDescription,
              confirmText: confirmLabel,
              initialPath: currentValue || placeholder,
              mode: pickerMode
            }).then((value) => {
              if (typeof value === 'string' && value.trim() !== '') {
                applyValue(value);
              }
            }).catch((err) => {
              console.error('Path picker widget failed:', err);
              if (canUseModalPrompt) {
                SDSM.modal.prompt({
                  title: 'Select Path',
                  label,
                  placeholder,
                  defaultValue,
                  confirmText: 'Apply'
                }).then((value) => {
                  if (value === null) return;
                  applyValue(value);
                }).catch((modalErr) => {
                  console.error('Fallback prompt modal failed:', modalErr);
                  fallbackPrompt();
                });
              } else {
                fallbackPrompt();
              }
            });
          } else if (canUseModalPrompt) {
            SDSM.modal.prompt({
              title: 'Select Path',
              label,
              placeholder,
              defaultValue,
              confirmText: 'Apply'
            }).then((value) => {
              if (value === null) return;
              applyValue(value);
            }).catch((err) => {
              console.error('Path picker modal failed:', err);
              fallbackPrompt();
            });
          } else {
            fallbackPrompt();
          }
          return;
        }

        const tokenTrigger = targetEl.closest('[data-token-popup]');
        if (tokenTrigger) {
          event.preventDefault();
          const context = tokenTrigger.getAttribute('data-token-popup') || 'server-defaults';
          const title = tokenTrigger.getAttribute('data-token-title') || 'Supported Tokens';
          const intro = tokenTrigger.getAttribute('data-token-intro') || 'Click a token to copy it to your clipboard.';
          if (SDSM.tokens && typeof SDSM.tokens.showReference === 'function') {
            SDSM.tokens.showReference(context, { title, intro, buttonText: tokenTrigger.getAttribute('data-token-button') || 'Close' });
          }
          return;
        }

        const pickerBtn = targetEl.closest('[data-show-picker]');
        if (pickerBtn) {
          if (pickerBtn.disabled) return;
          event.preventDefault();
          const targetId = pickerBtn.getAttribute('data-show-picker');
          if (!targetId) return;
          const input = document.getElementById(targetId);
          if (!input) return;
          try {
            if (typeof input.showPicker === 'function') {
              input.showPicker();
            } else {
              input.focus();
            }
          } catch (err) {
            input.focus();
          }
        }

        const historyBackBtn = targetEl.closest('[data-action="history-back"]');
        if (historyBackBtn) {
          event.preventDefault();
          if (window.history.length > 1) {
            window.history.back();
          } else {
            window.location.assign('/dashboard');
          }
        }
      });

      document.body.addEventListener('submit', (event) => {
        const form = event.target instanceof HTMLFormElement ? event.target : null;
        if (!form || form.id !== 'manager-settings-form') {
          return;
        }

        const submitter = event.submitter;
        if (submitter && submitter.name && submitter.name !== 'update_config') {
          return;
        }

        const steamIdInput = document.getElementById('steam_id');
        const rootPathInput = document.getElementById('root_path');
        const managerWebhookInput = document.getElementById('discord_manager_webhook');
        const defaultWebhookInput = document.getElementById('discord_default_webhook');

        const inputs = [steamIdInput, rootPathInput, managerWebhookInput, defaultWebhookInput].filter(Boolean);
        inputs.forEach((input) => {
          input.setCustomValidity('');
        });

        const isAbsolutePathLike = (value) => {
          const v = String(value || '').trim();
          if (!v) return false;
          if (v.startsWith('/')) return true;
          if (/^[a-zA-Z]:[\\/]/.test(v)) return true;
          if (v.startsWith('\\\\') || v.startsWith('//')) return true;
          return false;
        };

        const validateWebhook = (input) => {
          if (!input) return '';
          const raw = String(input.value || '').trim();
          if (!raw) return '';
          let parsed;
          try {
            parsed = new URL(raw);
          } catch (_) {
            return 'Webhook URL must be a valid URL.';
          }
          const protocol = String(parsed.protocol || '').toLowerCase();
          if (protocol !== 'https:' && protocol !== 'http:') {
            return 'Webhook URL must start with http:// or https://';
          }
          if (!parsed.host) {
            return 'Webhook URL must include a host.';
          }
          return '';
        };

        const errors = [];

        if (steamIdInput) {
          const steamID = String(steamIdInput.value || '').trim();
          if (!steamID) {
            const msg = 'Steam ID is required.';
            steamIdInput.setCustomValidity(msg);
            errors.push(steamIdInput);
          } else if (!/^\d{4,12}$/.test(steamID)) {
            const msg = 'Steam ID must be numeric (4-12 digits).';
            steamIdInput.setCustomValidity(msg);
            errors.push(steamIdInput);
          }
        }

        if (rootPathInput) {
          const rootPath = String(rootPathInput.value || '').trim();
          if (!rootPath) {
            const msg = 'Root Path is required.';
            rootPathInput.setCustomValidity(msg);
            errors.push(rootPathInput);
          } else if (!isAbsolutePathLike(rootPath)) {
            const msg = 'Root Path must be an absolute path.';
            rootPathInput.setCustomValidity(msg);
            errors.push(rootPathInput);
          }
        }

        const managerWebhookError = validateWebhook(managerWebhookInput);
        if (managerWebhookInput && managerWebhookError) {
          managerWebhookInput.setCustomValidity(managerWebhookError);
          errors.push(managerWebhookInput);
        }

        const defaultWebhookError = validateWebhook(defaultWebhookInput);
        if (defaultWebhookInput && defaultWebhookError) {
          defaultWebhookInput.setCustomValidity(defaultWebhookError);
          errors.push(defaultWebhookInput);
        }

        if (errors.length > 0) {
          event.preventDefault();
          event.stopPropagation();

          const first = errors[0];
          if (first && typeof first.reportValidity === 'function') {
            first.reportValidity();
          }
          if (first && typeof first.focus === 'function') {
            first.focus();
          }
          if (window.showToast) {
            window.showToast('Configuration Error', 'Please correct highlighted fields before saving.', 'danger');
          }
        }
      });

      document.body.addEventListener('change', (event) => {
        if (event.target && event.target.id === 'tls_enabled') {
          this.forms.syncTlsFields(event.target.checked);
        }
      });

      this.forms.syncTlsFields();

      this.collapses.init();
      this.logViewer.init();

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
        if (this.relativeTime && this.relativeTime.intervalId) {
          clearInterval(this.relativeTime.intervalId);
          this.relativeTime.intervalId = null;
        }
        if (this.dashboard) {
          this.dashboard.destroy();
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

function initServerCreationPage(root) {
  const scope = root && typeof root.querySelector === 'function' ? root : document;
  let form = null;
  if (scope && typeof scope.querySelector === 'function') {
    form = scope.querySelector('#serverForm');
    if (!form && scope.id === 'serverForm') {
      form = scope;
    }
  }
  if (!form) {
    form = document.getElementById('serverForm');
  }
  if (!form || form.dataset.serverFormInitialized === 'true') {
    return;
  }
  form.dataset.serverFormInitialized = 'true';

  const dropzone = form.querySelector('#saveDropzone');
  const saveFileInput = form.querySelector('#save_file');
  const saveFileName = form.querySelector('#saveFileName');
  const saveAnalysis = form.querySelector('#saveAnalysis');
  const detectedWorld = form.querySelector('#detectedWorld');
  const detectedName = form.querySelector('#detectedName');
  const basicNameGroup = form.querySelector('#basicNameGroup');
  const basicWorldGroup = form.querySelector('#basicWorldGroup');
  const nameTextInput = form.querySelector('#name_text');
  const worldSelect = form.querySelector('#world_select');
  const betaSelect = form.querySelector('#beta');
  const startLocationSelect = form.querySelector('#start_location');
  const startConditionSelect = form.querySelector('#start_condition');
  const difficultySelect = form.querySelector('#difficulty');
  const progressFill = form.querySelector('#progressFill');
  const progressText = form.querySelector('#progressText');
  const progressList = form.querySelector('#progressList');
  const submitButton = form.querySelector('button[type="submit"]');
  const tabButtons = form.querySelectorAll('.server-form-tab');
  const tabPanels = form.querySelectorAll('[data-tab-panel]');
  const presetButtons = form.querySelectorAll('.preset-btn');
  const portInput = form.querySelector('#port');
  const portUnlockButton = form.querySelector('[data-action="unlock-port"]');
  const portUnlockDefaultLabel = portUnlockButton ? (portUnlockButton.textContent || 'Customize') : '';
  const portUnlockUnlockedLabel = portUnlockButton?.dataset?.unlockedText || 'Unlocked';
  let suppressWorldChangeHandler = false;
  let suppressBetaChangeHandler = false;

  const requiredFields = ['name', 'world', 'start_location', 'start_condition', 'difficulty'];
  const totalRequired = requiredFields.length;

  const defaults = {
    world: form.dataset.defaultWorld || (worldSelect ? worldSelect.value : ''),
    startLocation: form.dataset.defaultStartLocation || '',
    startCondition: form.dataset.defaultStartCondition || '',
    beta: form.dataset.defaultBeta === 'true' ? 'true' : 'false',
  };

  const toCleanString = (value) => {
    if (typeof value === 'string') {
      return value.trim();
    }
    if (value === null || typeof value === 'undefined') {
      return '';
    }
    return `${value}`.trim();
  };

  const fallbackPresetLabel = (key) => {
    const cleaned = toCleanString(key);
    if (!cleaned) return 'Preset';
    return cleaned
      .replace(/[-_]+/g, ' ')
      .replace(/\b\w/g, (match) => match.toUpperCase());
  };

  const cloneFieldObject = (value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      return {};
    }
    return Object.keys(value).reduce((acc, fieldKey) => {
      acc[fieldKey] = value[fieldKey];
      return acc;
    }, {});
  };

  const normalizeCheckboxMap = (value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      return {};
    }
    return Object.keys(value).reduce((acc, fieldKey) => {
      const raw = value[fieldKey];
      if (typeof raw === 'boolean') {
        acc[fieldKey] = raw;
      } else if (typeof raw === 'string') {
        const lower = raw.trim().toLowerCase();
        if (lower === 'true' || lower === 'false') {
          acc[fieldKey] = lower === 'true';
        }
      } else if (typeof raw === 'number') {
        acc[fieldKey] = raw !== 0;
      }
      return acc;
    }, {});
  };

  const normalizePresetEntry = (preset) => {
    if (!preset || typeof preset !== 'object') {
      return null;
    }
    const rawKey = preset.key ?? preset.Key ?? '';
    const key = toCleanString(rawKey);
    if (!key) {
      return null;
    }
    const lookupKey = key.toLowerCase();
    const label = toCleanString(preset.label ?? preset.Label) || fallbackPresetLabel(key);
    const description = toCleanString(preset.description ?? preset.Description);
    const world = toCleanString(preset.world ?? preset.World);
    const startLocation = toCleanString(preset.start_location ?? preset.startLocation);
    const startCondition = toCleanString(preset.start_condition ?? preset.startCondition);
    const difficultyValue = toCleanString(preset.difficulty ?? preset.difficultyValue);
    const rawKeywords = preset.difficulty_keywords ?? preset.difficultyKeywords ?? [];
    const difficultyKeywords = Array.isArray(rawKeywords)
      ? rawKeywords.map((kw) => toCleanString(kw)).filter(Boolean)
      : [];
    const betaRaw = preset.beta ?? preset.Beta;
    let beta = null;
    if (typeof betaRaw === 'boolean') {
      beta = betaRaw;
    } else if (typeof betaRaw === 'string') {
      const lower = betaRaw.trim().toLowerCase();
      if (lower === 'true' || lower === 'false') {
        beta = lower === 'true';
      }
    }
    const fields = cloneFieldObject(preset.fields ?? preset.Fields);
    const checkboxes = normalizeCheckboxMap(preset.checkboxes ?? preset.Checkboxes);
    return {
      key,
      lookupKey,
      label,
      description,
      world,
      startLocation,
      startCondition,
      difficultyValue,
      difficultyKeywords,
      beta,
      fields,
      checkboxes,
    };
  };

  const parsePresetPayload = () => {
    const scriptEl = form.querySelector('#server-presets-data') || document.getElementById('server-presets-data');
    if (!scriptEl) {
      return null;
    }
    const raw = scriptEl.textContent || scriptEl.innerText || '';
    if (!raw.trim()) {
      return null;
    }
    try {
      return JSON.parse(raw);
    } catch (err) {
      console.error('Failed to parse preset payload', err);
      return null;
    }
  };

  const fallbackPresetList = () => ([
    {
      key: 'builder',
      label: 'Builder',
      world: 'Mars',
      start_condition: 'Standard',
      difficulty: 'Creative',
      difficulty_keywords: ['creative'],
      beta: false,
      fields: {
        max_clients: 6,
        save_interval: 120,
        welcome_message: 'Creative sandbox for collaborative builds.',
        welcome_back_message: 'Welcome back, visionary builder!'
      },
      checkboxes: {
        auto_save: true,
        player_saves: true,
        auto_pause: true,
        auto_start: false,
  		auto_update: true
      }
    },
    {
      key: 'beginner',
      label: 'Beginner',
      world: 'Mars',
      start_condition: 'Standard',
      difficulty: 'Easy',
      difficulty_keywords: ['easy', 'casual', 'standard', 'relaxed'],
      beta: false,
      fields: {
        max_clients: 8,
        save_interval: 180,
        welcome_message: 'Welcome aboard! Build at your own pace.',
        welcome_back_message: 'Welcome back, engineer!'
      },
      checkboxes: {
        auto_save: true,
        player_saves: true,
        auto_pause: true,
        auto_start: false,
  		auto_update: true
      }
    },
    {
      key: 'normal',
      label: 'Normal',
      world: 'Moon',
      start_condition: 'Standard',
      difficulty: 'Normal',
      difficulty_keywords: ['normal', 'default', 'standard'],
      beta: false,
      fields: {
        max_clients: 10,
        save_interval: 300,
        welcome_message: 'Welcome to our outpost. Have fun!',
        welcome_back_message: 'Suit up! Time to build.',
        welcome_delay_seconds: 1
      },
      checkboxes: {
        auto_save: true,
        player_saves: true,
        auto_pause: true,
        auto_start: true,
  		auto_update: true
      }
    },
    {
      key: 'hardcore',
      label: 'Hardcore',
      world: 'Vulcan',
      start_condition: 'Brutal',
      difficulty: 'Stationeer',
      difficulty_keywords: ['stationeer', 'hard', 'hardcore', 'survival', 'brutal'],
      beta: false,
      fields: {
        max_clients: 12,
        save_interval: 420,
        welcome_message: 'Hardcore server: no hand-holding.',
        welcome_back_message: "Back for more punishment? Let's go.",
        welcome_delay_seconds: 0
      },
      checkboxes: {
        auto_save: true,
        player_saves: false,
        auto_pause: false,
        auto_start: true,
  		auto_update: true
      }
    }
  ]);

  const buildPresetMap = () => {
    const parsed = parsePresetPayload();
    const source = Array.isArray(parsed) && parsed.length ? parsed : fallbackPresetList();
    const map = {};
    source.forEach((entry) => {
      const normalized = normalizePresetEntry(entry);
      if (normalized && normalized.lookupKey) {
        map[normalized.lookupKey] = normalized;
      }
    });
    return map;
  };

  const presets = buildPresetMap();

  const getBetaValue = () => {
    if (!betaSelect) return defaults.beta || 'false';
    return betaSelect.value === 'true' ? 'true' : 'false';
  };

  const setSubmitState = (submitting) => {
    if (!submitButton) return;
    if (submitting) {
      submitButton.dataset.originalText = submitButton.dataset.originalText || submitButton.textContent;
      submitButton.textContent = submitButton.dataset.loadingText || 'Creating...';
    } else if (submitButton.dataset.originalText) {
      submitButton.textContent = submitButton.dataset.originalText;
    }
    submitButton.disabled = submitting;
  };

  const setSingleOption = (selectEl, text) => {
    if (!selectEl) return;
    selectEl.innerHTML = '';
    const option = document.createElement('option');
    option.value = '';
    option.textContent = text;
    option.disabled = true;
    option.selected = true;
    selectEl.appendChild(option);
    selectEl.disabled = true;
  };

  const renderSelectOptions = (selectEl, items, preferredValue, placeholder) => {
    if (!selectEl) return;
    selectEl.disabled = false;
    selectEl.innerHTML = '';
    const placeholderOption = document.createElement('option');
    placeholderOption.value = '';
    placeholderOption.textContent = placeholder;
    placeholderOption.disabled = true;
    if (!preferredValue) {
      placeholderOption.selected = true;
    }
    selectEl.appendChild(placeholderOption);

    let matched = false;
    items.forEach((item) => {
      const id = item?.ID || item?.id || item?.value;
      if (!id) {
        return;
      }
      const option = document.createElement('option');
      option.value = id;
      option.textContent = item?.Name || item?.name || id;
      option.dataset.description = item?.Description || item?.description || '';
      if (preferredValue && preferredValue === id) {
        option.selected = true;
        matched = true;
      }
      selectEl.appendChild(option);
    });

    if (preferredValue && !matched && selectEl.options.length > 1) {
      selectEl.options[1].selected = true;
    }
  };

  const selectFirstAvailableOption = (selectEl) => {
    if (!selectEl || !selectEl.options?.length) {
      return '';
    }
    const first = Array.from(selectEl.options).find((opt) => opt && opt.value);
    if (!first) {
      return '';
    }
    const alreadySelected = selectEl.value === first.value;
    selectEl.value = first.value;
    if (!alreadySelected) {
      selectEl.dispatchEvent(new Event('input', { bubbles: true }));
      selectEl.dispatchEvent(new Event('change', { bubbles: true }));
    }
    return first.value;
  };

  const loadStartOptions = async (worldValue, betaValue, preferred = {}) => {
    if (!startLocationSelect || !startConditionSelect) {
      return;
    }
    if (!worldValue) {
      setSingleOption(startLocationSelect, 'Select a world to load locations');
      setSingleOption(startConditionSelect, 'Select a world to load conditions');
      return;
    }

    const query = new URLSearchParams();
    query.set('world', worldValue);
    query.set('beta', betaValue === 'true' ? 'true' : 'false');

    const {
      startLocation,
      startCondition,
      selectFirstStartLocation
    } = preferred || {};

    setSingleOption(startLocationSelect, 'Loading locations...');
    setSingleOption(startConditionSelect, 'Loading conditions...');

    try {
      const [locationsResp, conditionsResp] = await Promise.all([
        SDSM.api.request(`/api/start-locations?${query.toString()}`, { method: 'GET' }),
        SDSM.api.request(`/api/start-conditions?${query.toString()}`, { method: 'GET' })
      ]);
      const locationPreference = selectFirstStartLocation ? null : startLocation;
      renderSelectOptions(startLocationSelect, locationsResp?.locations || [], locationPreference, 'Select a start location');
      if (selectFirstStartLocation) {
        selectFirstAvailableOption(startLocationSelect);
      }
      renderSelectOptions(startConditionSelect, conditionsResp?.conditions || [], startCondition, 'Select a start condition');
    } catch (err) {
      console.error('Failed to load world data', err);
      setSingleOption(startLocationSelect, 'Unable to load start locations');
      setSingleOption(startConditionSelect, 'Unable to load start conditions');
    } finally {
      updateProgress();
    }
  };

  const ensureWorldOption = (worldValue, betaValue) => {
    if (!worldSelect || !worldValue) {
      return;
    }
    const existing = Array.from(worldSelect.options).find((opt) => opt.value === worldValue);
    if (existing) {
      existing.selected = true;
      return;
    }
    const option = new Option(worldValue, worldValue, true, true);
    option.dataset.beta = betaValue;
    worldSelect.appendChild(option);
  };

  const ensureWorldMatchesBeta = (betaValue) => {
    if (!worldSelect) {
      return '';
    }
    const desiredBeta = betaValue === 'true';
    const currentOption = worldSelect.options[worldSelect.selectedIndex];
    if (currentOption && (currentOption.dataset.beta === 'true') === desiredBeta) {
      return currentOption.value;
    }
    const fallback = Array.from(worldSelect.options).find((opt) => opt.value && (opt.dataset.beta === 'true') === desiredBeta);
    if (fallback) {
      fallback.selected = true;
      return fallback.value;
    }
    return worldSelect.value;
  };

  const findWorldOptionByPrefix = (worldName, betaValue) => {
    if (!worldSelect || !worldName) {
      return null;
    }
    const prefix = worldName.trim().toLowerCase();
    if (!prefix) {
      return null;
    }
    const desiredBeta = betaValue === 'true';
    const options = Array.from(worldSelect.options || []);
    return options.find((opt) => {
      if (!opt || !opt.value) {
        return false;
      }
      if (opt.dataset && typeof opt.dataset.beta !== 'undefined') {
        const optionBeta = opt.dataset.beta === 'true';
        if (optionBeta !== desiredBeta) {
          return false;
        }
      }
      const label = (opt.textContent || '').trim().toLowerCase();
      const value = (opt.value || '').trim().toLowerCase();
      return label.startsWith(prefix) || value.startsWith(prefix);
    }) || null;
  };

  const selectWorldOptionForPreset = (worldName, betaValue) => {
    const match = findWorldOptionByPrefix(worldName, betaValue);
    if (!match) {
      return null;
    }
    if (worldSelect.value !== match.value) {
      suppressWorldChangeHandler = true;
      worldSelect.value = match.value;
      worldSelect.dispatchEvent(new Event('input', { bubbles: true }));
      worldSelect.dispatchEvent(new Event('change', { bubbles: true }));
      suppressWorldChangeHandler = false;
    }
    return match.value;
  };

  const applyBetaValue = (betaValue) => {
    if (!betaSelect || typeof betaValue === 'undefined' || betaValue === null) {
      return;
    }
    const targetValue = betaValue ? 'true' : 'false';
    if (betaSelect.value === targetValue) {
      return;
    }
    suppressBetaChangeHandler = true;
    betaSelect.value = targetValue;
    betaSelect.dispatchEvent(new Event('input', { bubbles: true }));
    betaSelect.dispatchEvent(new Event('change', { bubbles: true }));
    suppressBetaChangeHandler = false;
  };

  const showFormTab = (target) => {
    if (!tabButtons.length || !tabPanels.length) {
      return;
    }
    const normalized = target === 'advanced' ? 'advanced' : 'basic';
    tabButtons.forEach((btn) => {
      const isActive = btn?.dataset?.tabTarget === normalized;
      btn.classList.toggle('is-active', Boolean(isActive));
      btn.setAttribute('role', 'tab');
      btn.setAttribute('aria-selected', isActive ? 'true' : 'false');
      btn.setAttribute('tabindex', isActive ? '0' : '-1');
    });
    tabPanels.forEach((panel) => {
      if (!panel || !panel.dataset) return;
      const matches = panel.dataset.tabPanel === normalized;
      panel.setAttribute('role', 'tabpanel');
      panel.classList.toggle('hidden', !matches);
      panel.setAttribute('aria-hidden', matches ? 'false' : 'true');

      const tab = Array.from(tabButtons).find((btn) => btn?.dataset?.tabTarget === panel.dataset.tabPanel);
      if (tab && tab.id) {
        panel.setAttribute('aria-labelledby', tab.id);
      }
    });
  };

  const setCheckboxValue = (name, value, { silent = false } = {}) => {
    if (typeof value === 'undefined' || value === null) return;
    const input = form.querySelector(`input[name="${name}"]`);
    if (input) {
      const nextState = Boolean(value);
      if (input.checked !== nextState) {
        input.checked = nextState;
        if (!silent) {
          input.dispatchEvent(new Event('input', { bubbles: true }));
          input.dispatchEvent(new Event('change', { bubbles: true }));
        }
      } else {
        input.checked = nextState;
      }
    }
  };

  const setInputValue = (id, value, { silent = false } = {}) => {
    if (typeof value === 'undefined' || value === null) return;
    const input = form.querySelector(`#${id}`);
    if (input) {
      input.value = value;
      if (!silent) {
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));
      }
    }
  };

  const setDifficultyValue = (value) => {
    if (!difficultySelect || !value || !difficultySelect.options?.length) {
      return false;
    }
    const normalized = value.toString().toLowerCase();
    const match = Array.from(difficultySelect.options).find((opt) => {
      const optValue = (opt.value || '').toLowerCase();
      const optLabel = (opt.textContent || '').toLowerCase();
      return optValue === normalized || optLabel === normalized;
    });
    if (match) {
      difficultySelect.value = match.value;
      difficultySelect.dispatchEvent(new Event('input', { bubbles: true }));
      difficultySelect.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    }
    return false;
  };

  const setDifficultyByKeywords = (keywords = []) => {
    if (!difficultySelect || !difficultySelect.options?.length) {
      return false;
    }
    const keyList = Array.isArray(keywords) ? keywords : [];
    const options = Array.from(difficultySelect.options);
    const normalizedOptions = options.map((opt) => ({
      value: opt.value,
      label: (opt.textContent || '').toLowerCase(),
    }));
    let chosen = null;
    keyList.some((keyword) => {
      const lowerKeyword = (keyword || '').toLowerCase();
      if (!lowerKeyword) return false;
      const match = normalizedOptions.find((opt) => opt.label.includes(lowerKeyword) || (opt.value || '').toLowerCase() === lowerKeyword);
      if (match) {
        chosen = match.value;
        return true;
      }
      return false;
    });
    if (!chosen && normalizedOptions.length) {
      chosen = normalizedOptions[0].value;
    }
    if (chosen) {
      difficultySelect.value = chosen;
      difficultySelect.dispatchEvent(new Event('input', { bubbles: true }));
      difficultySelect.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    }
    return false;
  };

  const unlockPortField = () => {
    if (!portInput) {
      return;
    }
    portInput.readOnly = false;
    portInput.classList.add('is-editable');
    portInput.dataset.portLock = 'false';
    if (portUnlockButton) {
      portUnlockButton.disabled = true;
      portUnlockButton.textContent = portUnlockUnlockedLabel;
    }
    portInput.focus();
  };

  const applySaveMetadata = (data) => {
    if (!data) return;
    const detectedWorldValue = data.world || '';
    const detectedNameValue = data.world_file_name || data.name || '';
    if (detectedWorld) {
      detectedWorld.textContent = detectedWorldValue || 'Unknown';
    }
    if (detectedName) {
      detectedName.textContent = detectedNameValue || '';
    }
    if (nameTextInput && detectedNameValue) {
      nameTextInput.value = detectedNameValue;
    }
    if (detectedWorldValue) {
      ensureWorldOption(detectedWorldValue, getBetaValue());
      worldSelect.value = detectedWorldValue;
      loadStartOptions(detectedWorldValue, getBetaValue(), {});
    }

    setInputValue('port', data.port);
    setInputValue('max_clients', data.max_clients);
    setInputValue('password', data.password);
    setInputValue('auth_secret', data.auth_secret);
    setInputValue('save_interval', data.save_interval);
    setInputValue('disconnect_timeout', data.disconnect_timeout);
    setCheckboxValue('server_visible', data.server_visible);
    setCheckboxValue('auto_save', data.auto_save);
    setCheckboxValue('auto_pause', data.auto_pause);
    setCheckboxValue('auto_start', data.auto_start);
    setCheckboxValue('auto_update', data.auto_update);
    setCheckboxValue('player_saves', data.player_saves);
    if (saveAnalysis) {
      saveAnalysis.classList.remove('hidden');
    }
    if (basicNameGroup) {
      basicNameGroup.classList.add('hidden');
    }
    if (basicWorldGroup) {
      basicWorldGroup.classList.add('hidden');
    }
    updateProgress();
  };

  const resetDropzone = () => {
    if (saveFileInput) {
      saveFileInput.value = '';
    }
    if (saveFileName) {
      saveFileName.textContent = '';
    }
    if (dropzone) {
      dropzone.classList.remove('dz-started', 'dz-dragover');
    }
    if (saveAnalysis) {
      saveAnalysis.classList.add('hidden');
    }
    if (basicNameGroup) {
      basicNameGroup.classList.remove('hidden');
    }
    if (basicWorldGroup) {
      basicWorldGroup.classList.remove('hidden');
    }
    if (detectedWorld) {
      detectedWorld.textContent = '';
    }
    if (detectedName) {
      detectedName.textContent = '';
    }
    updateProgress();
  };

  const handleFileSelect = async (file) => {
    if (!file) {
      resetDropzone();
      return;
    }
    if (!file.name.toLowerCase().endsWith('.save')) {
      if (window.showToast) {
        window.showToast('Invalid File', 'Please select a Stationeers .save file.', 'warning');
      }
      return;
    }
    if (saveFileName) {
      saveFileName.textContent = file.name;
    }
    if (dropzone) {
      dropzone.classList.add('dz-started');
    }

    const formData = new FormData();
    formData.append('save_file', file);

    try {
      const data = await SDSM.api.request('/api/servers/analyze-save', { method: 'POST', body: formData });
      applySaveMetadata(data);
    } catch (err) {
      console.error('Save analysis failed', err);
      if (window.showToast) {
        window.showToast('Error analyzing save', err.message || 'Failed to read save metadata.', 'danger');
      }
      resetDropzone();
    }
  };

  const updateProgress = () => {
    if (!progressFill || !progressText || !progressList) {
      return;
    }
    let completedCount = 0;
    const formData = new FormData(form);
    requiredFields.forEach((fieldName) => {
      const value = formData.get(fieldName);
      const progressItem = progressList.querySelector(`li[data-field="${fieldName}"]`);
      if (value) {
        completedCount += 1;
        if (progressItem) {
          progressItem.classList.add('completed');
        }
      } else if (progressItem) {
        progressItem.classList.remove('completed');
      }
    });
    const percentage = totalRequired > 0 ? (completedCount / totalRequired) * 100 : 0;
    progressFill.style.width = `${percentage}%`;
    progressText.textContent = `${completedCount} of ${totalRequired} required fields complete`;
  };

  const applyPreset = (key) => {
    if (!key) {
      return;
    }
    const lookup = key.toString().toLowerCase();
    const preset = presets[lookup] || presets[key];
    if (!presetButtons.length || !preset) {
      return;
    }
    const difficultySet = preset.difficultyValue ? setDifficultyValue(preset.difficultyValue) : false;
    if (!difficultySet && preset.difficultyKeywords) {
      setDifficultyByKeywords(preset.difficultyKeywords);
    }

    if (typeof preset.beta === 'boolean') {
      applyBetaValue(preset.beta);
    }

    const startPreferences = {
      startLocation: null,
      startCondition: preset.startCondition || null,
      selectFirstStartLocation: true,
    };

    if (worldSelect && preset.world) {
      const betaValue = typeof preset.beta === 'boolean' ? (preset.beta ? 'true' : 'false') : getBetaValue();
      const matchedValue = selectWorldOptionForPreset(preset.world, betaValue);
      const targetWorldValue = matchedValue || preset.world;
      if (!matchedValue) {
        ensureWorldOption(targetWorldValue, betaValue);
        if (worldSelect.value !== targetWorldValue) {
          suppressWorldChangeHandler = true;
          worldSelect.value = targetWorldValue;
          worldSelect.dispatchEvent(new Event('input', { bubbles: true }));
          worldSelect.dispatchEvent(new Event('change', { bubbles: true }));
          suppressWorldChangeHandler = false;
        }
      }
      loadStartOptions(targetWorldValue, betaValue, startPreferences);
    } else if (worldSelect && startPreferences && worldSelect.value) {
      loadStartOptions(worldSelect.value, getBetaValue(), startPreferences);
    }

    if (preset.fields) {
      Object.entries(preset.fields).forEach(([fieldId, value]) => {
        setInputValue(fieldId, value);
      });
    }
    if (preset.checkboxes) {
      Object.entries(preset.checkboxes).forEach(([fieldName, value]) => {
        setCheckboxValue(fieldName, value);
      });
    }
    presetButtons.forEach((btn) => {
      if (!btn || !btn.dataset) return;
      const buttonKey = (btn.dataset.preset || '').toLowerCase();
      const isActive = buttonKey === preset.lookupKey;
      btn.classList.toggle('is-active', Boolean(isActive));
      btn.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    });
    updateProgress();
  };

  if (dropzone && saveFileInput) {
    dropzone.addEventListener('click', () => saveFileInput.click());
    saveFileInput.addEventListener('change', () => handleFileSelect(saveFileInput.files[0]));
    dropzone.addEventListener('dragover', (event) => {
      event.preventDefault();
      dropzone.classList.add('dz-dragover');
    });
    dropzone.addEventListener('dragleave', () => dropzone.classList.remove('dz-dragover'));
    dropzone.addEventListener('drop', (event) => {
      event.preventDefault();
      dropzone.classList.remove('dz-dragover');
      const file = event.dataTransfer?.files?.[0];
      handleFileSelect(file);
    });
  }

  if (tabButtons.length && tabPanels.length) {
    tabButtons.forEach((btn, index) => {
      if (!btn.id) {
        btn.id = `server-form-tab-${index + 1}`;
      }
    });

    tabButtons.forEach((btn) => {
      if (!btn) return;
      btn.addEventListener('click', (event) => {
        event.preventDefault();
        const target = btn.dataset?.tabTarget || 'basic';
        showFormTab(target);
      });

      btn.addEventListener('keydown', (event) => {
        const tabs = Array.from(tabButtons);
        const currentIndex = tabs.indexOf(btn);
        if (currentIndex < 0) return;

        let nextIndex = -1;
        switch (event.key) {
          case 'ArrowLeft':
          case 'ArrowUp':
            nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
            break;
          case 'ArrowRight':
          case 'ArrowDown':
            nextIndex = (currentIndex + 1) % tabs.length;
            break;
          case 'Home':
            nextIndex = 0;
            break;
          case 'End':
            nextIndex = tabs.length - 1;
            break;
          default:
            return;
        }

        event.preventDefault();
        const nextTab = tabs[nextIndex];
        if (!nextTab) return;
        nextTab.focus();
        const target = nextTab.dataset?.tabTarget;
        if (target) {
          showFormTab(target);
        }
      });
    });
    showFormTab('basic');
  }

  if (presetButtons.length) {
    presetButtons.forEach((btn) => {
      if (!btn) return;
      btn.setAttribute('aria-pressed', 'false');
      btn.addEventListener('click', (event) => {
        event.preventDefault();
        const key = btn.dataset?.preset;
        if (key) {
          applyPreset(key);
        }
      });
    });
  }

  if (portUnlockButton && portInput) {
    portUnlockButton.addEventListener('click', (event) => {
      event.preventDefault();
      if (portInput.readOnly) {
        unlockPortField();
      }
    });
  }

  if (worldSelect) {
    worldSelect.addEventListener('change', () => {
      if (!suppressWorldChangeHandler) {
        loadStartOptions(worldSelect.value, getBetaValue(), {});
      }
      updateProgress();
    });
  }

  if (betaSelect) {
    betaSelect.addEventListener('change', () => {
      if (suppressBetaChangeHandler) {
        return;
      }
      const betaValue = getBetaValue();
      const matched = ensureWorldMatchesBeta(betaValue);
      loadStartOptions(matched || worldSelect?.value || '', betaValue, {});
    });
  }

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (submitButton && submitButton.disabled) {
      return;
    }
    const usingSaveUpload = Boolean(saveFileInput && saveFileInput.files && saveFileInput.files.length > 0);
    const endpoint = usingSaveUpload ? '/api/servers/create-from-save' : '/api/servers';
    const formData = new FormData(form);
    setSubmitState(true);
    try {
      const response = await SDSM.api.request(endpoint, { method: 'POST', body: formData });
      if (response && typeof response.server_id !== 'undefined') {
        window.location.href = `/server/${response.server_id}`;
      } else {
        window.location.href = '/dashboard';
      }
    } catch (err) {
      console.error('Server creation failed', err);
      if (window.showToast) {
        window.showToast('Error', err.message || 'Failed to create server.', 'danger');
      }
    } finally {
      setSubmitState(false);
    }
  });

  form.addEventListener('input', updateProgress);
  form.addEventListener('change', updateProgress);

  const initialWorld = ensureWorldMatchesBeta(defaults.beta) || defaults.world;
  if (worldSelect && initialWorld) {
    ensureWorldOption(initialWorld, defaults.beta);
    worldSelect.value = initialWorld;
  }
  loadStartOptions(worldSelect ? worldSelect.value : defaults.world, defaults.beta, {
    startLocation: defaults.startLocation,
    startCondition: defaults.startCondition,
  });
  updateProgress();

  form.addEventListener('reset', () => {
    setTimeout(() => {
      resetDropzone();
      loadStartOptions(worldSelect ? worldSelect.value : '', getBetaValue(), {});
      if (portInput) {
        portInput.readOnly = true;
        portInput.classList.remove('is-editable');
        portInput.dataset.portLock = 'true';
      }
      if (portUnlockButton) {
        portUnlockButton.disabled = false;
        portUnlockButton.textContent = portUnlockDefaultLabel || 'Customize';
      }
      if (tabButtons.length) {
        showFormTab('basic');
      }
      if (presetButtons.length) {
        presetButtons.forEach((btn) => {
          if (!btn) return;
          btn.classList.remove('is-active');
          btn.setAttribute('aria-pressed', 'false');
        });
      }
    }, 0);
  });
}

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
    initServerCreationPage(document);

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

    const applyLogTabSemantics = () => {
      const tabs = Array.from(logTabs.querySelectorAll('.tab[data-log-file]'));
      let activeTab = null;

      tabs.forEach((tab, index) => {
        if (!tab.id) {
          tab.id = `${logTabs.id}-tab-${index + 1}`;
        }
        tab.setAttribute('role', 'tab');
        tab.setAttribute('aria-controls', 'logContent');
        const isActive = tab.classList.contains('active');
        tab.setAttribute('aria-selected', isActive ? 'true' : 'false');
        tab.setAttribute('tabindex', isActive ? '0' : '-1');
        if (isActive) {
          activeTab = tab;
        }
      });

      if (logContent) {
        logContent.setAttribute('role', 'tabpanel');
        if (activeTab) {
          logContent.setAttribute('aria-labelledby', activeTab.id);
        } else {
          logContent.removeAttribute('aria-labelledby');
        }
      }
    };

    const setActiveLogTab = (tab) => {
      if (!tab) return;
      const tabs = Array.from(logTabs.querySelectorAll('.tab[data-log-file]'));
      tabs.forEach((candidate) => {
        const isActive = candidate === tab;
        candidate.classList.toggle('active', isActive);
        candidate.setAttribute('aria-selected', isActive ? 'true' : 'false');
        candidate.setAttribute('tabindex', isActive ? '0' : '-1');
      });
      if (logContent) {
        if (tab.id) {
          logContent.setAttribute('aria-labelledby', tab.id);
        } else {
          logContent.removeAttribute('aria-labelledby');
        }
      }
    };

        function fetchLogFiles() {
            htmx.ajax('GET', '/api/logs', {
                target: '#logTabs',
                swap: 'innerHTML'
            }).then((e) => {
                if (e.detail.xhr.status < 400) {
                    if (logTabs.children.length > 1) { // more than just the empty state
                        logTabsEmpty.classList.add('hidden');
            applyLogTabSemantics();
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
              setActiveLogTab(tab);
                    const logFile = tab.dataset.logFile;
                    fetchLogContent(logFile);
                }
            });

          logTabs.addEventListener('keydown', (e) => {
            const current = e.target.closest('.tab[data-log-file]');
            if (!current) return;
            const tabs = Array.from(logTabs.querySelectorAll('.tab[data-log-file]'));
            if (!tabs.length) return;
            const index = tabs.indexOf(current);
            if (index < 0) return;

            let nextIndex = -1;
            switch (e.key) {
              case 'Enter':
              case ' ':
                e.preventDefault();
                setActiveLogTab(current);
                {
                  const logFile = current.dataset.logFile;
                  fetchLogContent(logFile);
                }
                return;
              case 'ArrowLeft':
              case 'ArrowUp':
                nextIndex = (index - 1 + tabs.length) % tabs.length;
                break;
              case 'ArrowRight':
              case 'ArrowDown':
                nextIndex = (index + 1) % tabs.length;
                break;
              case 'Home':
                nextIndex = 0;
                break;
              case 'End':
                nextIndex = tabs.length - 1;
                break;
              default:
                return;
            }

            e.preventDefault();
            const nextTab = tabs[nextIndex];
            if (!nextTab) return;
            nextTab.focus();
            setActiveLogTab(nextTab);
            const logFile = nextTab.dataset.logFile;
            fetchLogContent(logFile);
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
            if (!activeLogFile) {
              return;
            }

            const clearNow = () => {
              htmx.ajax('POST', `/api/logs/${activeLogFile}/clear`, {}).then(() => {
                fetchLogContent(activeLogFile);
              });
            };

            const hasConfirmTemplate = !!document.getElementById('tpl-modal-confirm');
            const canUseModalConfirm = Boolean(window.SDSM && SDSM.modal && typeof SDSM.modal.confirm === 'function' && hasConfirmTemplate);
            const message = `Are you sure you want to clear the log file "${activeLogFile}"?`;

            if (canUseModalConfirm) {
              SDSM.modal.confirm({
                title: 'Clear Log File',
                body: message,
                confirmText: 'Clear Log',
                cancelText: 'Cancel',
                danger: true,
              }).then((confirmed) => {
                if (confirmed) {
                  clearNow();
                }
              }).catch(() => {
                if (window.confirm(message)) {
                  clearNow();
                }
              });
              return;
            }

            if (window.confirm(message)) {
              clearNow();
            }
            });
        }

        // Initial load
        fetchLogFiles();
        updateButtonStates();
    }
});
