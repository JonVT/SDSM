function initServerStatusDashboard() {
    const serverContent = document.querySelector('.server-content');
    if (!serverContent) {
        console.error('Server content container not found. Aborting script.');
        return;
    }

    const serverId = serverContent.dataset.serverId;
    const serverName = serverContent.dataset.serverName || '';
    const serverApiBase = `/api/servers/${serverId}`;
    const hasApiHelper = window.SDSM && SDSM.api && typeof SDSM.api.request === 'function';
    const hasApiDownloadHelper = window.SDSM && SDSM.api && typeof SDSM.api.downloadWorld === 'function';

    let lastKnownRunning = serverContent.dataset.serverRunning === 'true';

    const playerSavesEnabled = serverContent.dataset.playerSaves === 'true';
    const playerSaveExcludes = new Set(parseCSVList(serverContent.dataset.playerSaveExcludes || ''));
    const bannedIds = new Set(parseCSVList(serverContent.dataset.bannedIds || ''));

    const configWorldsData = readJSONScript('server-config-worlds-data') || {};
    const configWorldMeta = readJSONScript('server-config-world-meta') || {};
    const configDifficulties = readJSONScript('server-config-difficulties') || {};

    const userRole = (document.body?.dataset?.role || '').toLowerCase();
    const isAdminUser = userRole === 'admin';

    let statusRefreshTimer;

    function parseCSVList(value) {
        if (!value) {
            return [];
        }
        return value
            .split(',')
            .map((entry) => entry.trim())
            .filter((entry) => entry.length > 0);
    }

    function readJSONScript(id) {
        const el = document.getElementById(id);
        if (!el) {
            return null;
        }
        const text = (el.textContent || '').trim();
        if (!text) {
            return null;
        }
        try {
            return JSON.parse(text);
        } catch (error) {
            console.warn(`Unable to parse JSON from #${id}`, error);
            return null;
        }
    }

    function refreshFeatherIcons() {
        if (window.feather && typeof window.feather.replace === 'function') {
            window.feather.replace();
        }
    }

    async function serverRequest(path, options = {}) {
        const url = `${serverApiBase}${path}`;
        if (hasApiHelper) {
            return SDSM.api.request(url, options);
        }

        const {
            method = 'POST',
            body,
            headers = {},
        } = options;

        const fetchHeaders = {
            Accept: 'application/json',
            'HX-Request': 'true',
            ...headers,
        };

        let payload = body;
        if (body && !(body instanceof FormData)) {
            fetchHeaders['Content-Type'] = fetchHeaders['Content-Type'] || 'application/json';
            payload = JSON.stringify(body);
        }

        const resp = await fetch(url, {
            method,
            headers: fetchHeaders,
            body: payload,
            credentials: 'same-origin',
        });
        const text = await resp.text();
        if (!resp.ok) {
            const message = (() => {
                try {
                    const parsed = JSON.parse(text);
                    return parsed?.error || resp.statusText || 'Request failed';
                } catch (_) {
                    return text || resp.statusText || 'Request failed';
                }
            })();
            if (window.showToast) {
                window.showToast('Error', message, 'danger');
            }
            throw new Error(message);
        }
        try {
            return JSON.parse(text || '{}');
        } catch (_) {
            return {};
        }
    }

    function handleActionError(action, error) {
        console.error(`${action} failed`, error);
        if (!hasApiHelper && window.showToast && error?.message) {
            window.showToast('Error', error.message, 'danger');
        }
    }

    function buildQuery(params = {}) {
        const search = new URLSearchParams();
        Object.entries(params).forEach(([key, value]) => {
            if (value !== undefined && value !== null && value !== '') {
                search.append(key, value);
            }
        });
        const query = search.toString();
        return query ? `?${query}` : '';
    }

    function formatDateTime(value) {
        if (!value) {
            return '—';
        }
        const date = value instanceof Date ? value : new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '—';
        }
        return date.toLocaleString();
    }

    function formatDurationFromStart(startISO) {
        if (!startISO) {
            return '0m';
        }
        const start = new Date(startISO);
        if (Number.isNaN(start.getTime())) {
            return '0m';
        }
        let diff = Date.now() - start.getTime();
        if (diff <= 0) {
            return '0m';
        }
        const mins = Math.floor(diff / 60000);
        if (mins < 1) {
            return `${Math.max(1, Math.floor(diff / 1000))}s`;
        }
        const hours = Math.floor(mins / 60);
        const days = Math.floor(hours / 24);
        if (days > 0) {
            const remHours = hours % 24;
            return `${days}d ${String(remHours).padStart(2, '0')}h`;
        }
        if (hours > 0) {
            const remMins = mins % 60;
            return `${hours}h ${String(remMins).padStart(2, '0')}m`;
        }
        return `${mins}m`;
    }

    let uptimeTimerId;

    function refreshTimingFromDataset() {
        if (!serverContent) {
            return;
        }
        if (startedAtEl) {
            startedAtEl.textContent = formatDateTime(serverContent.dataset.serverStarted);
        }
        if (uptimeEl) {
            const startedISO = serverContent.dataset.serverStarted;
            uptimeEl.textContent = serverContent.dataset.serverRunning === 'true' && startedISO
                ? formatDurationFromStart(startedISO)
                : '0m';
        }
        if (lastSavedEl) {
            lastSavedEl.textContent = formatDateTime(serverContent.dataset.serverSaved);
        }
        const lastLogValue = serverContent.dataset.serverLastLog;
        updateLastLogDisplay(lastLogValue);
        updateLatestSaveSummary();
    }

    function updateLatestSaveSummary() {
        if (!latestSaveSummary) {
            return;
        }
        const currentName = (serverContent?.dataset?.serverName || serverName || '').trim();
        if (latestSaveNameEl) {
            latestSaveNameEl.textContent = currentName ? `${currentName}.save` : 'Latest save unavailable';
        }
        if (latestSavePathEl) {
            latestSavePathEl.textContent = currentName
                ? `saves/${currentName}/${currentName}.save`
                : 'saves/<server>/<server>.save';
        }
        if (latestSaveButton) {
            if (currentName) {
                const filename = `${currentName}.save`;
                latestSaveButton.dataset.saveFilename = filename;
                latestSaveButton.dataset.saveLabel = filename;
                latestSaveButton.disabled = false;
            } else {
                latestSaveButton.dataset.saveFilename = '';
                latestSaveButton.dataset.saveLabel = '';
                latestSaveButton.disabled = true;
            }
        }
        if (latestSaveTimestampEl) {
            const savedISO = serverContent.dataset.serverSaved;
            latestSaveTimestampEl.textContent = savedISO ? formatDateTime(savedISO) : 'Not saved yet';
        }
    }

    function startUptimeTicker() {
        if (uptimeTimerId) {
            clearInterval(uptimeTimerId);
        }
        const startedISO = serverContent?.dataset?.serverStarted;
        if (!startedISO || serverContent.dataset.serverRunning !== 'true') {
            return;
        }
        const tick = () => {
            if (uptimeEl) {
                uptimeEl.textContent = formatDurationFromStart(serverContent.dataset.serverStarted);
            }
        };
        tick();
        uptimeTimerId = setInterval(tick, 30000);
    }

    function stopUptimeTicker() {
        if (uptimeTimerId) {
            clearInterval(uptimeTimerId);
            uptimeTimerId = null;
        }
    }

    function updateLastLogDisplay(text) {
        if (!lastLogEl) {
            return;
        }
        const trimmed = (text || '').trim();
        lastLogEl.textContent = trimmed || 'No log data captured yet.';
    }

    function updateStormDisplay(isStorming) {
        const active = !!isStorming;
        if (stormPill) {
            stormPill.classList.toggle('is-storm', active);
            stormPill.classList.toggle('is-clear', !active);
            stormPill.textContent = active ? 'Storming' : 'Calm';
        }
        if (stormStatusText) {
            stormStatusText.textContent = active ? 'Storm active' : 'Calm skies';
        }
        if (btnStartStorm) {
            btnStartStorm.disabled = !lastKnownRunning || active;
        }
        if (btnStopStorm) {
            btnStopStorm.disabled = !lastKnownRunning || !active;
        }
    }

    function syncCleanupAvailability() {
        if (!cleanupButtons.length) {
            return;
        }
        const disabled = serverContent.dataset.serverRunning !== 'true';
        cleanupButtons.forEach((btn) => {
            btn.disabled = disabled;
        });
    }
    
    const statusIndicator = document.getElementById('server-status-indicator');
    const statusText = statusIndicator ? statusIndicator.querySelector('.status-text') : null;

    const btnStart = document.getElementById('btn-start');
    const btnStop = document.getElementById('btn-stop');
    const btnPause = document.getElementById('btn-pause');
    const btnResume = document.getElementById('btn-resume');
        const worldDownloadSelect = document.getElementById('world-download-select');
        const worldDownloadStatus = document.getElementById('world-download-status');
    const btnSave = document.getElementById('btn-save');
    const btnQuickSave = document.getElementById('btn-quicksave');
    const btnUpdate = document.getElementById('btn-update');
    const btnReinstall = document.getElementById('btn-reinstall');
    const btnUploadWorld = document.getElementById('btn-upload-world');
    const btnDownloadWorld = document.getElementById('btn-download-world');
    const btnRenameServer = document.getElementById('btn-rename-server');
    const languageSelect = document.getElementById('server-language-select');
    const startedAtEl = document.getElementById('server-started-at');
    const uptimeEl = document.getElementById('server-uptime');
    const lastSavedEl = document.getElementById('server-last-saved');
    const lastLogEl = document.getElementById('server-last-log');
    const latestSaveSummary = document.getElementById('latest-save-summary');
    const latestSaveNameEl = document.getElementById('latest-save-name');
    const latestSavePathEl = document.getElementById('latest-save-path');
    const latestSaveTimestampEl = document.getElementById('latest-save-timestamp');
    const latestSaveButton = document.getElementById('btn-load-latest-save');
    const stormPill = document.getElementById('storm-status-pill');
    const stormStatusText = document.getElementById('storm-status-text');
    const btnStartStorm = document.getElementById('btn-start-storm');
    const btnStopStorm = document.getElementById('btn-stop-storm');
    const cleanupButtons = Array.from(document.querySelectorAll('[data-cleanup-scope]'));
    const consoleForm = document.getElementById('server-console-form');
    const consoleInput = document.getElementById('server-console-command');
    const consoleSubmit = document.getElementById('server-console-submit');
    const logsButton = document.getElementById('btn-open-logs');

    if (lastLogEl && !serverContent.dataset.serverLastLog) {
        const initialLog = (lastLogEl.textContent || '').trim();
        if (initialLog && initialLog !== 'No log data captured yet.') {
            serverContent.dataset.serverLastLog = initialLog;
        }
    }

    const chatLog = document.getElementById('chat-log');
    const chatForm = document.getElementById('chat-form');
    const chatInput = document.getElementById('chat-input');
    const chatSend = document.getElementById('chat-send');

    function formatBytes(bytes) {
        if (typeof bytes !== 'number' || !isFinite(bytes) || bytes < 0) {
            return '';
        }
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        let idx = 0;
        let value = bytes;
        while (value >= 1024 && idx < units.length - 1) {
            value /= 1024;
            idx++;
        }
        return `${value.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
    }

    let chatEmpty = document.getElementById('chat-empty');

    const savesList = document.getElementById('saves-list');
    const savesEmpty = document.getElementById('saves-empty');
    const savesTabs = document.getElementById('saves-tabs');
    let currentSavesFilter = 'all';
    let activePlayerSaveGroupKey = null;
    const SAVE_FILTER_MAP = {
        all: ['auto', 'quick', 'manual', 'player'],
        auto: ['auto'],
        quick: ['quick'],
        manual: ['manual'],
        player: ['player'],
    };
    const SAVE_TYPE_LABELS = {
        auto: 'Auto Save',
        quick: 'Quick Save',
        manual: 'Named Save',
        player: 'Player Save',
    };
    const SAVE_TYPE_ICONS = {
        auto: 'clock',
        quick: 'zap',
        manual: 'file-text',
        player: 'user',
    };

    const livePlayersTable = document.getElementById('live-players-table');
    const livePlayersEmpty = document.getElementById('live-players-empty');
    const liveCount = document.getElementById('live-count');

    async function refreshWorldDownloadOptions() {
            if (!worldDownloadSelect) {
                return;
            }
            worldDownloadSelect.innerHTML = '<option value="">Loading saves...</option>';
            worldDownloadSelect.disabled = true;
            if (worldDownloadStatus) {
                worldDownloadStatus.textContent = 'Loading save list…';
            }
            try {
                const data = await serverRequest('/world/saves', { method: 'GET' });
                const files = Array.isArray(data.files) ? data.files : [];
                worldDownloadSelect.innerHTML = '';
                const placeholder = document.createElement('option');
                placeholder.value = '';
                placeholder.textContent = files.length ? 'Latest save (automatic)' : 'No save files available';
                worldDownloadSelect.appendChild(placeholder);
                if (!files.length) {
                    worldDownloadSelect.disabled = true;
                    if (worldDownloadStatus) {
                        worldDownloadStatus.textContent = 'No save files available yet.';
                    }
                    return;
                }
                files.forEach(file => {
                    if (!file || !file.name) {
                        return;
                    }
                    const option = document.createElement('option');
                    option.value = file.name;
                    const parts = [file.name];
                    if (file.modified) {
                        parts.push(new Date(file.modified).toLocaleString());
                    }
                    if (typeof file.size_bytes === 'number') {
                        const sizeLabel = formatBytes(file.size_bytes);
                        if (sizeLabel) {
                            parts.push(sizeLabel);
                        }
                    }
                    option.textContent = parts.join(' • ');
                    worldDownloadSelect.appendChild(option);
                });
                worldDownloadSelect.disabled = false;
                if (worldDownloadStatus) {
                    const newest = files[0]?.modified ? new Date(files[0].modified).toLocaleString() : '';
                    worldDownloadStatus.textContent = newest ? `Newest save: ${newest}` : 'Select a save to download.';
                }
            } catch (error) {
                worldDownloadSelect.innerHTML = '<option value="">Unable to load saves</option>';
                if (worldDownloadStatus) {
                    worldDownloadStatus.textContent = 'Unable to load save list.';
                }
                handleActionError('World Saves', error);
            }
        }

    refreshWorldDownloadOptions();

    function buildWorldDownloadUrl() {
        if (!worldDownloadSelect) {
            return `${serverApiBase}/world/download`;
        }
        const selectedName = worldDownloadSelect.disabled ? '' : (worldDownloadSelect.value || '').trim();
        const query = selectedName ? buildQuery({ name: selectedName }) : '';
        return `${serverApiBase}/world/download${query}`;
    }

    const historyPlayersTable = document.getElementById('history-players-table');
    const historyPlayersEmpty = document.getElementById('history-players-empty');
    const historyCount = document.getElementById('history-count');
    const expandedHistoryGroups = new Set();
    let historyViewMode = 'player';
    let latestHistoryEntries = [];
    const historyViewToggleButtons = Array.from(document.querySelectorAll('.history-view-toggle'));
    const activeHistoryToggle = historyViewToggleButtons.find((btn) => btn.classList.contains('active') && btn.dataset.historyView);
    if (activeHistoryToggle) {
        historyViewMode = activeHistoryToggle.dataset.historyView;
    }
    historyViewToggleButtons.forEach((button) => {
        button.addEventListener('click', () => {
            const view = button.dataset.historyView || 'player';
            if (historyViewMode === view) {
                return;
            }
            historyViewMode = view;
            historyViewToggleButtons.forEach((btn) => {
                btn.classList.toggle('active', btn === button);
            });
            renderHistoryList();
        });
    });
    if (historyPlayersTable) {
        historyPlayersTable.addEventListener('click', (event) => {
            if (historyViewMode !== 'player') {
                return;
            }
            const playerCell = event.target.closest('.history-group-row .player-cell');
            if (!playerCell) {
                return;
            }
            const row = playerCell.closest('.history-group-row');
            if (!row) {
                return;
            }
            const key = row.dataset.historyGroupKey;
            if (!key) {
                return;
            }
            const isAlreadyExpanded = expandedHistoryGroups.has(key);
            expandedHistoryGroups.clear();
            if (!isAlreadyExpanded) {
                expandedHistoryGroups.add(key);
            }
            updateHistoryGroupExpansionStates();
        });
    }

    function updateHistoryGroupExpansionStates() {
        if (!historyPlayersTable) {
            return;
        }
        const rows = historyPlayersTable.querySelectorAll('.history-group-row');
        rows.forEach((row) => {
            const key = row.dataset.historyGroupKey;
            const isExpanded = key ? expandedHistoryGroups.has(key) : false;
            row.classList.toggle('collapsed', !isExpanded);
            const sessionList = row.querySelector('.player-session-list');
            if (sessionList) {
                sessionList.classList.toggle('collapsed', !isExpanded);
            }
            const summary = row.querySelector('.player-session-summary');
            if (summary) {
                summary.classList.toggle('hidden', isExpanded);
            }
        });
    }
    const bannedPlayersTable = document.getElementById('banned-players-table');
    const bannedPlayersEmpty = document.getElementById('banned-players-empty');
    const bannedCount = document.getElementById('banned-count');

    const serverConfigForm = document.getElementById('server-config-form');
    const serverConfigContent = document.getElementById('serverConfigContent');
    const serverConfigToggle = document.getElementById('serverConfigToggle');
    const serverLogsContent = document.getElementById('serverLogsContent');
    const serverLogsToggle = document.getElementById('serverLogsToggle');
    const serverConfigCard = document.getElementById('server-config-card');
    const configVersionSelect = document.getElementById('config-version');
    const configWorldSelect = document.getElementById('config-world');
    const configStartLocationSelect = document.getElementById('config-start-location');
    const configStartConditionSelect = document.getElementById('config-start-condition');
    const configDifficultySelect = document.getElementById('config-difficulty');

    const logViewer = document.getElementById('log-viewer');
    const logTabs = document.getElementById('log-tabs');
    const logTabsEmpty = document.getElementById('log-tabs-empty');
    const slRefresh = document.getElementById('sl-refresh');
    const slDownload = document.getElementById('sl-download');
    const slClear = document.getElementById('sl-clear');

    let socket;
    let reconnectAttempts = 0;
    const maxReconnectAttempts = 5;
    const reconnectInterval = 2000;

    function connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        socket = new WebSocket(`${protocol}//${window.location.host}/ws/server/${serverId}`);

        socket.onopen = () => {
            console.log('WebSocket connected');
            reconnectAttempts = 0;
        };

        socket.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                handleWebSocketMessage(data);
            } catch (e) {
                console.error("Error parsing websocket message", e);
            }
        };

        socket.onclose = () => {
            console.log('WebSocket disconnected');
            if (reconnectAttempts < maxReconnectAttempts) {
                setTimeout(connectWebSocket, reconnectInterval * (reconnectAttempts + 1));
                reconnectAttempts++;
            } else if (window.showToast) {
                window.showToast('Error', 'Could not reconnect to server. Please refresh the page.', 'danger');
            }
        };

        socket.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    function handleWebSocketMessage(data) {
        if (!data || !data.type) {
            return;
        }
        switch (data.type) {
            case 'server_status':
                if (data.serverId && String(data.serverId) !== String(serverId)) {
                    return;
                }
                if (data.status) {
                    updateServerStatus(data.status);
                }
                break;
            case 'status':
                if (data.payload) {
                    updateServerStatus(data.payload);
                }
                break;
            case 'chat':
                addChatMessage(data.payload || data);
                break;
            case 'players':
                if (data.payload) {
                    updatePlayerLists({
                        live: normalizeLivePlayers(data.payload.live),
                        history: normalizeHistoryPlayers(data.payload.history),
                        banned: normalizeBannedPlayers(data.payload.banned),
                    });
                }
                break;
            case 'saves':
                fetchSaves(currentSavesFilter);
                break;
            case 'manager_progress': {
                const detail = data.snapshot || {};
                document.dispatchEvent(new CustomEvent('sdsm:manager-progress', { detail }));
                break;
            }
            case 'stats_update': {
                const detail = data.stats || {};
                document.dispatchEvent(new CustomEvent('sdsm:stats-update', { detail }));
                break;
            }
            case 'log':
                // handled elsewhere
                break;
            case 'error':
                showToast('Error', (data.payload && data.payload.message) || 'Server reported an error.', 'danger');
                break;
            default:
                break;
        }
    }

    function updateServerStatus(status) {
        serverContent.dataset.serverRunning = status.running ? 'true' : 'false';
        serverContent.dataset.serverPaused = status.paused ? 'true' : 'false';
        serverContent.dataset.serverStarting = status.starting ? 'true' : 'false';
        serverContent.dataset.serverStopping = status.stopping ? 'true' : 'false';
        if (typeof status.storming !== 'undefined') {
            serverContent.dataset.serverStorming = status.storming ? 'true' : 'false';
            updateStormDisplay(status.storming);
        }

        const wasRunning = lastKnownRunning;
        const isRunning = !!status.running;
        lastKnownRunning = isRunning;

        let statusClass = 'status-stopped';
        let text = 'Stopped';

        if (status.starting) {
            statusClass = 'status-starting';
            text = 'Starting';
        } else if (status.stopping) {
            statusClass = 'status-stopping';
            text = 'Stopping';
        } else if (status.running) {
            if (status.paused) {
                statusClass = 'status-paused';
                text = 'Paused';
            } else {
                statusClass = 'status-running';
                text = 'Running';
            }
        }
        
        if(statusIndicator) {
            statusIndicator.className = `status-indicator ${statusClass}`;
        }
        if(statusText) {
            statusText.textContent = text;
        }

        if(btnStart) btnStart.disabled = status.running || status.starting;
        if(btnStop) btnStop.disabled = !status.running || status.stopping;
        if(btnPause) btnPause.disabled = !status.running || status.paused;
        if(btnResume) btnResume.disabled = !status.paused;
        if(btnSave) btnSave.disabled = !status.running;
        if(btnQuickSave) btnQuickSave.disabled = !status.running;
        if(btnUpdate) btnUpdate.disabled = status.running;
        if(btnReinstall) btnReinstall.disabled = status.running;
        if(btnUploadWorld) btnUploadWorld.disabled = status.running;
        if(chatInput) chatInput.disabled = !status.running;
        if(chatSend) chatSend.disabled = !status.running;
        syncCleanupAvailability();
        if (consoleInput) consoleInput.disabled = !status.running;
        if (consoleSubmit) consoleSubmit.disabled = !status.running;
        if (typeof status.storming !== 'undefined') {
            updateStormDisplay(status.storming);
        }
        
        const configSubmitButton = serverConfigForm ? serverConfigForm.querySelector('button[type="submit"]') : null;
        if(configSubmitButton) {
            configSubmitButton.disabled = status.running;
        }

        if (wasRunning && !isRunning) {
            fetchSaves(currentSavesFilter);
            refreshWorldDownloadOptions();
        }
        if (isRunning) {
            startUptimeTicker();
        } else {
            stopUptimeTicker();
        }
    }

    function applyInitialStatusFromDataset() {
        if (!serverContent) {
            return;
        }
        const datasetStatus = {
            running: serverContent.dataset.serverRunning === 'true',
            paused: serverContent.dataset.serverPaused === 'true',
            starting: serverContent.dataset.serverStarting === 'true',
            stopping: serverContent.dataset.serverStopping === 'true',
            storming: serverContent.dataset.serverStorming === 'true'
        };
        updateServerStatus(datasetStatus);
        refreshTimingFromDataset();
        if (datasetStatus.running) {
            startUptimeTicker();
        } else {
            stopUptimeTicker();
        }
        updateStormDisplay(datasetStatus.storming);
    }

    document.addEventListener('sdsm:server-status', (event) => {
        const detail = event.detail || {};
        if (!detail || String(detail.serverId) !== String(serverId)) {
            return;
        }
        if (detail.status) {
            updateServerStatus({
                running: !!detail.status.running,
                paused: !!detail.status.paused,
                starting: !!detail.status.starting,
                stopping: !!detail.status.stopping,
                storming: serverContent.dataset.serverStorming === 'true'
            });
        }
    });

    function hydrateStatusDetails(data) {
        if (!data) {
            return;
        }
        if (Array.isArray(data.banned)) {
            const ids = data.banned.map((entry) => entry?.SteamID || entry?.steam_id || '').filter(Boolean);
            replaceSetContents(bannedIds, ids);
        }
        if (Array.isArray(data.players) || Array.isArray(data.clients) || Array.isArray(data.banned)) {
            updatePlayerLists({
                live: normalizeLivePlayers(data.players),
                history: normalizeHistoryPlayers(data.clients),
                banned: normalizeBannedPlayers(data.banned),
            });
        }
        if (Array.isArray(data.chat_messages)) {
            renderChatMessages(data.chat_messages);
        }
    }

    async function fetchLatestStatus() {
        try {
            const data = await serverRequest('/status', { method: 'GET' });
            const payload = {
                running: !!data.running,
                paused: !!data.paused,
                starting: !!data.starting,
                stopping: !!data.stopping,
                storming: !!data.storming
            };
            serverContent.dataset.serverRunning = payload.running ? 'true' : 'false';
            serverContent.dataset.serverPaused = payload.paused ? 'true' : 'false';
            serverContent.dataset.serverStarting = payload.starting ? 'true' : 'false';
            serverContent.dataset.serverStopping = payload.stopping ? 'true' : 'false';
            if (typeof data.storming !== 'undefined') {
                serverContent.dataset.serverStorming = data.storming ? 'true' : 'false';
            }
            if (data.server_started) {
                serverContent.dataset.serverStarted = data.server_started;
            } else {
                delete serverContent.dataset.serverStarted;
            }
            if (data.server_saved) {
                serverContent.dataset.serverSaved = data.server_saved;
            } else {
                delete serverContent.dataset.serverSaved;
            }
            if (typeof data.last_log_line !== 'undefined') {
                const trimmed = (data.last_log_line || '').trim();
                if (trimmed) {
                    serverContent.dataset.serverLastLog = trimmed;
                } else {
                    delete serverContent.dataset.serverLastLog;
                }
            }
            hydrateStatusDetails(data);
            updateServerStatus(payload);
            refreshTimingFromDataset();
            if (payload.running) {
                startUptimeTicker();
            } else {
                stopUptimeTicker();
            }
        } catch (error) {
            console.warn('Unable to refresh server status', error);
        }
    }

    function startStatusRefreshLoop() {
        if (statusRefreshTimer) {
            clearInterval(statusRefreshTimer);
        }
        statusRefreshTimer = setInterval(fetchLatestStatus, 60000);
    }

    function addChatMessage(message) {
        if (!chatLog) {
            return;
        }
        hideChatEmptyState();
        const entry = buildChatMessageElement(message);
        if (!entry) {
            return;
        }
        chatLog.appendChild(entry);
        chatLog.scrollTop = chatLog.scrollHeight;
    }

    function renderChatMessages(messages) {
        if (!chatLog) {
            return;
        }
        chatLog.innerHTML = '';
        chatEmpty = null;
        const normalized = Array.isArray(messages) ? messages.map(normalizeChatMessage).filter(Boolean) : [];
        if (!normalized.length) {
            showChatEmptyState();
            return;
        }
        hideChatEmptyState();
        const fragment = document.createDocumentFragment();
        normalized.forEach((msg) => {
            const entry = buildChatMessageElement(msg);
            if (entry) {
                fragment.appendChild(entry);
            }
        });
        chatLog.appendChild(fragment);
        chatLog.scrollTop = chatLog.scrollHeight;
    }

    function buildChatMessageElement(rawMessage) {
        const message = normalizeChatMessage(rawMessage);
        if (!message) {
            return null;
        }
        const entry = document.createElement('article');
        entry.className = 'chat-message';
        if (message.timestamp) {
            entry.dataset.timestamp = message.timestamp;
        }
        const timestampSpan = document.createElement('span');
        timestampSpan.className = 'chat-timestamp';
        timestampSpan.textContent = formatChatTimestamp(message.timestamp);
        const authorSpan = document.createElement('span');
        authorSpan.className = 'chat-author';
        authorSpan.textContent = message.author ? `${message.author}:` : 'Server:';
        const textSpan = document.createElement('span');
        textSpan.className = 'chat-text';
        textSpan.textContent = message.text || '';
        entry.appendChild(timestampSpan);
        entry.appendChild(authorSpan);
        entry.appendChild(textSpan);
        return entry;
    }

    function formatChatTimestamp(value) {
        if (!value) {
            return '';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '';
        }
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    function normalizeChatMessage(entry) {
        if (!entry) {
            return null;
        }
        return {
            author: entry.author || entry.player || entry.name || entry.Author || entry.Player || 'Server',
            text: entry.text || entry.message || entry.Message || '',
            timestamp: entry.timestamp || entry.time || entry.datetime || entry.Datetime || entry.Timestamp || '',
        };
    }

    function showChatEmptyState() {
        if (!chatLog) {
            return;
        }
        if (!chatEmpty) {
            chatEmpty = document.createElement('div');
            chatEmpty.id = 'chat-empty';
            chatEmpty.className = 'empty-state';
            chatEmpty.innerHTML = `
                <div class="empty-icon"><i data-feather="message-square"></i></div>
                <h3 class="empty-state-title">Chat is quiet</h3>
                <p class="empty-state-description">Messages will appear here once the server starts broadcasting chat logs.</p>
            `;
            chatLog.appendChild(chatEmpty);
            refreshFeatherIcons();
        }
        chatEmpty.classList.remove('hidden');
    }

    function hideChatEmptyState() {
        if (chatEmpty) {
            chatEmpty.classList.add('hidden');
        }
    }

    function updatePlayerTable(tableBody, emptyState, players, rowRenderer) {
        if (!tableBody || typeof rowRenderer !== 'function') {
            return;
        }
        tableBody.innerHTML = '';
        const list = Array.isArray(players) ? players.filter(Boolean) : [];
        if (!list.length) {
            if (emptyState) {
                emptyState.classList.remove('hidden');
            }
            return;
        }
        if (emptyState) {
            emptyState.classList.add('hidden');
        }
        const fragment = document.createDocumentFragment();
        list.forEach((player) => {
            const row = rowRenderer(player);
            if (row) {
                fragment.appendChild(row);
            }
        });
        tableBody.appendChild(fragment);
    }
    
    function updatePlayerLists(players) {
        if (!players) {
            return;
        }
        updatePlayerTable(livePlayersTable, livePlayersEmpty, players.live, renderLivePlayerRow);
        if (liveCount) {
            liveCount.textContent = players.live ? players.live.length : 0;
        }

        latestHistoryEntries = Array.isArray(players.history) ? players.history : [];
        renderHistoryList();

        if (bannedPlayersTable) {
            updatePlayerTable(bannedPlayersTable, bannedPlayersEmpty, players.banned, renderBannedPlayerRow);
        }
        if (bannedCount) {
            bannedCount.textContent = players.banned ? players.banned.length : 0;
        }
        refreshFeatherIcons();
    }

    function renderLivePlayerRow(player) {
        if (!player) {
            return null;
        }
        const row = document.createElement('tr');
        row.className = 'player-row';
        const steamId = player.steamId || '';
        if (steamId) {
            row.dataset.steamId = steamId;
            row.dataset.guid = steamId;
        }
        const saveState = getPlayerSaveState(steamId);
        row.dataset.playerSave = saveState.state;

        const nameCell = document.createElement('td');
        nameCell.className = 'player-cell';
        const nameDiv = document.createElement('div');
        nameDiv.className = 'player-name';
        nameDiv.textContent = player.name || 'Unknown Player';
        nameCell.appendChild(nameDiv);

        const meta = document.createElement('div');
        meta.className = 'player-meta';
        const steamSpan = document.createElement('span');
        steamSpan.className = 'player-meta-item';
        steamSpan.textContent = steamId ? `Steam: ${steamId}` : 'Steam ID unavailable';
        meta.appendChild(steamSpan);
        if (player.isAdmin) {
            const adminPill = document.createElement('span');
            adminPill.className = 'player-pill is-admin';
            adminPill.title = 'Server administrator';
            adminPill.textContent = 'Admin';
            meta.appendChild(adminPill);
        }
        nameCell.appendChild(meta);
        row.appendChild(nameCell);

        const connectionCell = document.createElement('td');
        connectionCell.className = 'player-connection';
        const joined = document.createElement('div');
        joined.className = 'player-meta-item';
        joined.textContent = `Joined ${formatPlayerTimestamp(player.connectedAt)}`;
        const session = document.createElement('div');
        session.className = 'player-meta-item';
        session.textContent = `Session ${formatSessionDurationLabel(player.connectedAt)}`;
        connectionCell.appendChild(joined);
        connectionCell.appendChild(session);
        row.appendChild(connectionCell);

        const statusCell = document.createElement('td');
        statusCell.className = 'player-status';
        const savePill = document.createElement('span');
        savePill.className = saveState.className;
        if (saveState.icon) {
            savePill.innerHTML = `<i data-feather="${saveState.icon}"></i> ${saveState.label}`;
        } else {
            savePill.textContent = saveState.label;
        }
        if (saveState.title) {
            savePill.title = saveState.title;
        }
        statusCell.appendChild(savePill);
        row.appendChild(statusCell);

        if (isAdminUser) {
            const actionsCell = document.createElement('td');
            actionsCell.className = 'player-actions text-right';
            const kickBtn = document.createElement('button');
            kickBtn.className = 'btn btn-icon btn-sm btn-danger btn-kick';
            kickBtn.innerHTML = '<i data-feather="user-x"></i>';
            kickBtn.title = steamId ? 'Kick player' : 'Steam ID required';
            if (!steamId) {
                kickBtn.disabled = true;
            } else {
                kickBtn.dataset.steamId = steamId;
                kickBtn.dataset.guid = steamId;
            }
            const banBtn = document.createElement('button');
            banBtn.className = 'btn btn-icon btn-sm btn-warning btn-ban';
            banBtn.innerHTML = '<i data-feather="slash"></i>';
            banBtn.title = steamId ? 'Ban player' : 'Steam ID required';
            if (!steamId) {
                banBtn.disabled = true;
            } else {
                banBtn.dataset.steamId = steamId;
                banBtn.dataset.guid = steamId;
            }
            actionsCell.appendChild(kickBtn);
            actionsCell.appendChild(banBtn);
            row.appendChild(actionsCell);
        }

        return row;
    }

    function renderHistoryPlayerRow(player) {
        if (!player) {
            return null;
        }
        const row = document.createElement('tr');
        row.className = 'player-row';
        const steamId = player.steamId || '';
        if (steamId) {
            row.dataset.steamId = steamId;
            row.dataset.guid = steamId;
        }
        const saveState = getPlayerSaveState(steamId);
        row.dataset.playerSave = saveState.state;
        const isBanned = steamId && bannedIds.has(steamId);

        const nameCell = document.createElement('td');
        nameCell.className = 'player-cell';
        const nameDiv = document.createElement('div');
        nameDiv.className = 'player-name';
        nameDiv.textContent = player.name || 'Unknown Player';
        nameCell.appendChild(nameDiv);

        const meta = document.createElement('div');
        meta.className = 'player-meta';
        if (steamId) {
            const steamSpan = document.createElement('span');
            steamSpan.className = 'player-meta-item';
            steamSpan.textContent = `Steam: ${steamId}`;
            meta.appendChild(steamSpan);
        }
        if (player.isAdmin) {
            const adminPill = document.createElement('span');
            adminPill.className = 'player-pill is-admin';
            adminPill.textContent = 'Admin';
            meta.appendChild(adminPill);
        }
        if (isBanned) {
            const bannedPill = document.createElement('span');
            bannedPill.className = 'player-pill is-banned';
            bannedPill.textContent = 'Banned';
            meta.appendChild(bannedPill);
        }
        nameCell.appendChild(meta);
        row.appendChild(nameCell);

        const connectionCell = document.createElement('td');
        connectionCell.className = 'player-connection';
        const joined = document.createElement('div');
        joined.className = 'player-meta-item';
        joined.textContent = `Joined ${formatPlayerTimestamp(player.connectedAt)}`;
        const lastSeen = document.createElement('div');
        lastSeen.className = 'player-meta-item';
        lastSeen.textContent = player.disconnectAt ? `Last seen ${formatPlayerTimestamp(player.disconnectAt)}` : 'Currently online';
        connectionCell.appendChild(joined);
        connectionCell.appendChild(lastSeen);
        row.appendChild(connectionCell);

        const statusCell = document.createElement('td');
        statusCell.className = 'player-status';
        const savePill = document.createElement('span');
        savePill.className = saveState.className;
        if (saveState.icon) {
            savePill.innerHTML = `<i data-feather="${saveState.icon}"></i> ${saveState.label}`;
        } else {
            savePill.textContent = saveState.label;
        }
        if (saveState.title) {
            savePill.title = saveState.title;
        }
        statusCell.appendChild(savePill);
        row.appendChild(statusCell);

        if (isAdminUser) {
            const actionsCell = document.createElement('td');
            actionsCell.className = 'player-actions text-right';
            const banBtn = document.createElement('button');
            banBtn.className = 'btn btn-icon btn-sm btn-warning btn-ban';
            banBtn.innerHTML = '<i data-feather="slash"></i>';
            banBtn.title = steamId ? 'Ban player' : 'Steam ID required';
            if (!steamId || isBanned) {
                banBtn.disabled = true;
            }
            if (steamId) {
                banBtn.dataset.steamId = steamId;
                banBtn.dataset.guid = steamId;
            }
            actionsCell.appendChild(banBtn);
            row.appendChild(actionsCell);
        }

        return row;
    }

    function renderHistoryList() {
        if (!historyPlayersTable) {
            return;
        }
        const entries = Array.isArray(latestHistoryEntries) ? latestHistoryEntries : [];
        if (!entries.length) {
            historyPlayersTable.innerHTML = '';
            if (historyPlayersEmpty) {
                historyPlayersEmpty.classList.remove('hidden');
            }
            if (historyCount) {
                historyCount.textContent = '0';
            }
            return;
        }
        if (historyViewMode === 'player') {
            if (historyPlayersEmpty) {
                historyPlayersEmpty.classList.add('hidden');
            }
            const groups = groupHistoryEntriesByPlayer(entries);
            historyPlayersTable.innerHTML = '';
            const fragment = document.createDocumentFragment();
            groups.forEach((group) => {
                const row = renderHistoryPlayerGroupRow(group);
                if (row) {
                    fragment.appendChild(row);
                }
            });
            historyPlayersTable.appendChild(fragment);
            if (historyCount) {
                historyCount.textContent = String(groups.length);
            }
        } else {
            const sortedSessions = sortHistorySessions(entries);
            updatePlayerTable(historyPlayersTable, historyPlayersEmpty, sortedSessions, renderHistoryPlayerRow);
            if (historyCount) {
                historyCount.textContent = String(sortedSessions.length);
            }
        }
        refreshFeatherIcons();
    }

    function renderHistoryPlayerGroupRow(group) {
        if (!group) {
            return null;
        }
        const row = document.createElement('tr');
        row.className = 'player-row history-group-row';
        if (group.steamId) {
            row.dataset.steamId = group.steamId;
            row.dataset.guid = group.steamId;
        }
        if (group.key) {
            row.dataset.historyGroupKey = group.key;
        }
        const saveState = getPlayerSaveState(group.steamId);
        row.dataset.playerSave = saveState.state;
        const isBanned = group.steamId && bannedIds.has(group.steamId);
        const isExpanded = group.key ? expandedHistoryGroups.has(group.key) : false;
        if (!isExpanded) {
            row.classList.add('collapsed');
        }

        const nameCell = document.createElement('td');
        nameCell.className = 'player-cell';
        const nameDiv = document.createElement('div');
        nameDiv.className = 'player-name';
        nameDiv.textContent = group.name || 'Unknown Player';
        nameCell.appendChild(nameDiv);
        const meta = document.createElement('div');
        meta.className = 'player-meta';
        if (group.steamId) {
            const steamSpan = document.createElement('span');
            steamSpan.className = 'player-meta-item';
            steamSpan.textContent = `Steam: ${group.steamId}`;
            meta.appendChild(steamSpan);
        }
        if (group.isAdmin) {
            const adminPill = document.createElement('span');
            adminPill.className = 'player-pill is-admin';
            adminPill.textContent = 'Admin';
            meta.appendChild(adminPill);
        }
        if (isBanned) {
            const bannedPill = document.createElement('span');
            bannedPill.className = 'player-pill is-banned';
            bannedPill.textContent = 'Banned';
            meta.appendChild(bannedPill);
        }
        nameCell.appendChild(meta);
        row.appendChild(nameCell);

        const sessionCell = document.createElement('td');
        sessionCell.className = 'player-session-history';
        const sessionList = document.createElement('div');
        sessionList.className = 'player-session-list';
        if (!isExpanded) {
            sessionList.classList.add('collapsed');
        }
        group.sessions.forEach((session) => {
            const item = buildHistorySessionListItem(session);
            if (item) {
                sessionList.appendChild(item);
            }
        });
        const summary = buildHistorySessionSummaryElement(group);
        if (summary) {
            summary.classList.toggle('hidden', isExpanded);
            sessionCell.appendChild(summary);
        }
        sessionCell.appendChild(sessionList);
        row.appendChild(sessionCell);

        const statusCell = document.createElement('td');
        statusCell.className = 'player-status';
        const savePill = document.createElement('span');
        savePill.className = saveState.className;
        if (saveState.icon) {
            savePill.innerHTML = `<i data-feather="${saveState.icon}"></i> ${saveState.label}`;
        } else {
            savePill.textContent = saveState.label;
        }
        if (saveState.title) {
            savePill.title = saveState.title;
        }
        statusCell.appendChild(savePill);
        row.appendChild(statusCell);

        if (isAdminUser) {
            const actionsCell = document.createElement('td');
            actionsCell.className = 'player-actions text-right';
            const banBtn = document.createElement('button');
            banBtn.className = 'btn btn-icon btn-sm btn-warning btn-ban';
            banBtn.innerHTML = '<i data-feather="slash"></i>';
            banBtn.title = group.steamId ? 'Ban player' : 'Steam ID required';
            if (!group.steamId || isBanned) {
                banBtn.disabled = true;
            }
            if (group.steamId) {
                banBtn.dataset.steamId = group.steamId;
                banBtn.dataset.guid = group.steamId;
            }
            actionsCell.appendChild(banBtn);
            row.appendChild(actionsCell);
        }

        return row;
    }

    function buildHistorySessionListItem(session) {
        if (!session) {
            return null;
        }
        const item = document.createElement('div');
        item.className = 'player-session-item';
        const heading = document.createElement('div');
        heading.className = 'player-session-item-heading';
        heading.innerHTML = `
            <span>${formatPlayerTimestamp(session.connectedAt)}</span>
            <span>${session.disconnectAt ? formatPlayerTimestamp(session.disconnectAt) : 'Active'}</span>
        `;
        const body = document.createElement('div');
        body.className = 'player-session-item-body';
        const lengthLabel = session.sessionLength || formatSessionDurationLabel(session.connectedAt);
        body.textContent = lengthLabel ? `Duration: ${lengthLabel}` : '';
        item.appendChild(heading);
        if (lengthLabel) {
            item.appendChild(body);
        }
        return item;
    }

    function buildHistorySessionSummaryElement(group) {
        if (!group) {
            return null;
        }
        const summary = document.createElement('div');
        summary.className = 'player-session-summary';
        const firstRow = document.createElement('div');
        firstRow.className = 'player-session-summary-row';
        const firstLabel = document.createElement('span');
        firstLabel.className = 'label';
        firstLabel.textContent = 'First Session';
        const firstValue = document.createElement('span');
        firstValue.className = 'value';
        firstValue.textContent = formatPlayerTimestamp(group.oldestStart) || '—';
        firstRow.appendChild(firstLabel);
        firstRow.appendChild(firstValue);

        const lastRow = document.createElement('div');
        lastRow.className = 'player-session-summary-row';
        const lastLabel = document.createElement('span');
        lastLabel.className = 'label';
        lastLabel.textContent = 'Last Session';
        const lastValue = document.createElement('span');
        lastValue.className = 'value';
        lastValue.textContent = formatPlayerTimestamp(group.latestStart) || '—';
        lastRow.appendChild(lastLabel);
        lastRow.appendChild(lastValue);

        summary.appendChild(firstRow);
        summary.appendChild(lastRow);
        return summary;
    }

    function groupHistoryEntriesByPlayer(entries) {
        const map = new Map();
        entries.forEach((entry) => {
            if (!entry) {
                return;
            }
            const normalizedKey = entry.steamId || `name:${entry.name || 'unknown'}`;
            if (!map.has(normalizedKey)) {
                map.set(normalizedKey, {
                    key: normalizedKey,
                    steamId: entry.steamId || '',
                    name: entry.name || 'Unknown Player',
                    isAdmin: !!entry.isAdmin,
                    sessions: [],
                });
            }
            const group = map.get(normalizedKey);
            group.sessions.push(entry);
            if (entry.isAdmin) {
                group.isAdmin = true;
            }
        });
        const groups = Array.from(map.values());
        groups.forEach((group) => {
            group.sessions = sortHistorySessions(group.sessions);
            group.latestStart = group.sessions[0]?.connectedAt || '';
            group.oldestStart = group.sessions[group.sessions.length - 1]?.connectedAt || '';
        });
        groups.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
        return groups;
    }

    function sortHistorySessions(sessions) {
        return [...sessions].sort((a, b) => getHistorySessionTimestamp(b) - getHistorySessionTimestamp(a));
    }

    function getHistorySessionTimestamp(entry) {
        if (!entry) {
            return 0;
        }
        return toTimestamp(entry.disconnectAt) || toTimestamp(entry.connectedAt);
    }

    function toTimestamp(value) {
        if (!value) {
            return 0;
        }
        const date = new Date(value);
        return Number.isNaN(date.getTime()) ? 0 : date.getTime();
    }

    function renderBannedPlayerRow(player) {
        if (!player || !isAdminUser) {
            return null;
        }
        const row = document.createElement('tr');
        row.className = 'player-row';
        const steamId = player.steamId || '';
        if (steamId) {
            row.dataset.steamId = steamId;
            row.dataset.guid = steamId;
        }

        const idCell = document.createElement('td');
        const idDiv = document.createElement('div');
        idDiv.className = 'player-name';
        idDiv.textContent = steamId || 'Unknown ID';
        idCell.appendChild(idDiv);
        row.appendChild(idCell);

        const nameCell = document.createElement('td');
        nameCell.textContent = player.name || 'Unknown Player';
        row.appendChild(nameCell);

        const actionsCell = document.createElement('td');
        actionsCell.className = 'player-actions text-right';
        const unbanBtn = document.createElement('button');
        unbanBtn.className = 'btn btn-icon btn-sm btn-success btn-unban';
        unbanBtn.innerHTML = '<i data-feather="check-circle"></i>';
        unbanBtn.title = 'Remove ban';
        if (steamId) {
            unbanBtn.dataset.steamId = steamId;
            unbanBtn.dataset.guid = steamId;
        } else {
            unbanBtn.disabled = true;
            unbanBtn.title = 'Steam ID unavailable';
        }
        actionsCell.appendChild(unbanBtn);
        row.appendChild(actionsCell);

        return row;
    }

    function getPlayerSaveState(steamId) {
        if (!playerSavesEnabled) {
            return { state: 'disabled', label: 'Manual Saves Only', className: 'player-pill is-save-disabled', title: 'Player saves disabled' };
        }
        if (!steamId) {
            return { state: 'disabled', label: 'Steam ID Needed', className: 'player-pill is-save-disabled', title: 'Steam ID required for player saves' };
        }
        if (playerSaveExcludes.has(steamId)) {
            return { state: 'excluded', label: 'Save Excluded', className: 'player-pill is-save-disabled', icon: 'slash', title: 'Excluded from automatic player saves' };
        }
        return { state: 'enabled', label: 'Save Active', className: 'player-pill is-save-enabled', icon: 'save', title: 'Player saves enabled' };
    }

    function formatPlayerTimestamp(value) {
        if (!value) {
            return '—';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '—';
        }
        return date.toLocaleString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' });
    }

    function formatSessionDurationLabel(startISO, fallback = '0m') {
        if (!startISO) {
            return fallback;
        }
        const label = formatDurationFromStart(startISO);
        return label || fallback;
    }

    function normalizeLivePlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                name: entry.Name || entry.name || entry.Player || 'Unknown Player',
                steamId: entry.SteamID || entry.steam_id || entry.guid || entry.GUID || '',
                isAdmin: !!(entry.IsAdmin ?? entry.is_admin),
                connectedAt: entry.ConnectDatetime || entry.connected_at || entry.connectedAt || entry.connected || '',
            };
        }).filter(Boolean);
    }

    function normalizeHistoryPlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                name: entry.Name || entry.name || entry.Player || 'Unknown Player',
                steamId: entry.SteamID || entry.steam_id || entry.guid || entry.GUID || '',
                isAdmin: !!(entry.IsAdmin ?? entry.is_admin),
                connectedAt: entry.ConnectDatetime || entry.connected_at || entry.connectedAt || entry.connected || '',
                disconnectAt: entry.DisconnectDatetime || entry.disconnect_at || entry.disconnectAt || '',
                sessionLength: entry.SessionDurationString || entry.session_length || entry.session || '',
            };
        }).filter(Boolean);
    }

    function normalizeBannedPlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                steamId: entry.SteamID || entry.steam_id || entry.guid || '',
                name: entry.Name || entry.name || '',
            };
        }).filter(Boolean);
    }

    function replaceSetContents(targetSet, values) {
        if (!targetSet) {
            return;
        }
        targetSet.clear();
        if (Array.isArray(values)) {
            values.forEach((value) => {
                if (value) {
                    targetSet.add(value);
                }
            });
        }
        if (serverContent) {
            serverContent.dataset.bannedIds = Array.from(targetSet).join(',');
        }
    }

    function syncPlayerSaveExcludeDataset() {
        if (serverContent) {
            serverContent.dataset.playerSaveExcludes = Array.from(playerSaveExcludes).join(',');
        }
    }

    function getPlayerSaveGroupKey(group, index = 0) {
        if (!group) {
            return `group-${index}`;
        }
        if (group.steamId) {
            return `steam:${group.steamId}`;
        }
        const slug = (group.playerName || '').toLowerCase().replace(/[^a-z0-9]+/g, '-') || `player-${index}`;
        return `name:${slug}-${index}`;
    }

    function ensureActivePlayerSaveGroupKey(groups) {
        if (!activePlayerSaveGroupKey) {
            return;
        }
        const stillExists = Array.isArray(groups) && groups.some((group, index) => getPlayerSaveGroupKey(group, index) === activePlayerSaveGroupKey);
        if (!stillExists) {
            activePlayerSaveGroupKey = null;
        }
    }

    function setActivePlayerSaveGroup(key) {
        activePlayerSaveGroupKey = key || null;
        if (!savesList) {
            return;
        }
        const groupElements = Array.from(savesList.querySelectorAll('.player-save-group'));
        groupElements.forEach((el) => {
            const isActive = !!activePlayerSaveGroupKey && el.dataset.groupKey === activePlayerSaveGroupKey;
            el.classList.toggle('is-active', isActive);
            const toggleBtn = el.querySelector('.player-save-toggle');
            if (toggleBtn) {
                toggleBtn.setAttribute('aria-expanded', isActive ? 'true' : 'false');
                toggleBtn.setAttribute('aria-label', isActive ? 'Collapse player saves' : 'Expand player saves');
            }
        });
    }


    function updateSavesList(saves) {
        if (!savesList) {
            return;
        }
        savesList.classList.remove('player-groups-view');
        savesList.innerHTML = '';

        if (!Array.isArray(saves) || saves.length === 0) {
            if (savesEmpty) {
                savesEmpty.classList.remove('hidden');
            }
            return;
        }

        if (savesEmpty) {
            savesEmpty.classList.add('hidden');
        }

        const fragment = document.createDocumentFragment();
        saves.forEach((save) => {
            const row = buildSaveRow(save);
            if (row) {
                fragment.appendChild(row);
            }
        });
        savesList.appendChild(fragment);
        refreshFeatherIcons();
    }

    function buildSaveRow(save) {
        if (!save || !save.filename) {
            return null;
        }
        const label = save.label || save.name || save.filename.replace(/\.save$/i, '');
        const metaParts = [];
        if (save.typeLabel) {
            metaParts.push(save.typeLabel);
        }
        if (save.playerName || save.steamId) {
            const playerLabel = save.playerName ? `${save.playerName}${save.steamId ? ` (${save.steamId})` : ''}` : save.steamId;
            metaParts.push(playerLabel);
        }
        const row = document.createElement('div');
        row.className = 'save-row';
        row.dataset.saveType = save.type || 'manual';
        if (label) {
            const tooltip = metaParts.length ? `${label} • ${metaParts.join(' • ')}` : label;
            row.title = tooltip;
        }

        const typeCell = document.createElement('div');
        typeCell.className = 'save-row-type';
        const iconName = SAVE_TYPE_ICONS[save.type] || 'save';
        typeCell.innerHTML = `<i data-feather="${iconName}"></i>`;
        typeCell.title = save.typeLabel || 'Save';
        row.appendChild(typeCell);

        if (currentSavesFilter === 'player' && (save.playerName || save.steamId)) {
            row.classList.add('has-player');
            const playerCell = document.createElement('div');
            playerCell.className = 'save-row-player';
            playerCell.textContent = save.playerName || save.steamId || 'Unknown Player';
            row.appendChild(playerCell);
        }

        if (currentSavesFilter === 'manual' && (save.type || '') === 'manual') {
            row.classList.add('has-label');
            const labelCell = document.createElement('div');
            labelCell.className = 'save-row-label';
            labelCell.textContent = label || 'Named Save';
            row.appendChild(labelCell);
        }

        const dateCell = document.createElement('div');
        dateCell.className = 'save-row-date';
        dateCell.textContent = formatSaveDate(save.datetime);
        if (!label && metaParts.length) {
            dateCell.title = metaParts.join(' • ');
        }
        row.appendChild(dateCell);

        const actionsCell = document.createElement('div');
        actionsCell.className = 'save-actions';
        const loadBtn = document.createElement('button');
        loadBtn.className = 'btn btn-sm btn-secondary btn-load-save';
        loadBtn.dataset.saveFilename = save.filename;
        loadBtn.dataset.saveType = save.type;
        loadBtn.dataset.saveLabel = label;
        loadBtn.innerHTML = '<i data-feather="download"></i> Load';
        loadBtn.title = 'Load this save';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'btn btn-sm btn-danger btn-delete-save';
        deleteBtn.dataset.saveFilename = save.filename;
        deleteBtn.dataset.saveType = save.type;
        deleteBtn.dataset.saveLabel = label;
        deleteBtn.innerHTML = '<i data-feather="trash-2"></i> Delete';
        deleteBtn.title = 'Delete this save';
        actionsCell.appendChild(loadBtn);
        actionsCell.appendChild(deleteBtn);
        row.appendChild(actionsCell);

        return row;
    }

    function formatSaveDate(value) {
        if (!value) {
            return 'Unknown time';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return 'Unknown time';
        }
        return date.toLocaleString();
    }

    function normalizeSaveItems(type, data) {
        if (!data) return [];
        if (type === 'player') {
            const groups = Array.isArray(data.groups) ? data.groups : [];
            const normalizedPlayers = [];
            groups.forEach(group => {
                const items = Array.isArray(group.items) ? group.items : [];
                items.forEach(item => {
                    normalizedPlayers.push({
                        type: 'player',
                        filename: item.filename,
                        label: item.filename ? item.filename.replace(/\.save$/i, '') : 'Player Save',
                        datetime: item.datetime,
                        typeLabel: SAVE_TYPE_LABELS.player,
                        playerName: group.name || '',
                        steamId: group.steam_id || '',
                    });
                });
            });
            return normalizedPlayers;
        }

        const items = Array.isArray(data.items) ? data.items : [];
        return items.map(item => ({
            type: type === 'manual' ? 'manual' : type,
            filename: item.filename,
            label: item.name || (item.filename ? item.filename.replace(/\.save$/i, '') : 'Save'),
            datetime: item.datetime,
            typeLabel: SAVE_TYPE_LABELS[type] || 'Save',
        }));
    }

    function normalizePlayerSaveGroups(data) {
        const groups = Array.isArray(data?.groups) ? data.groups : [];
        return groups.map((group) => {
            const steamId = group?.steam_id || '';
            const playerName = group?.name || steamId || 'Unknown Player';
            const saves = (Array.isArray(group?.items) ? group.items : [])
                .map((item) => ({
                    type: 'player',
                    filename: item.filename,
                    label: item.name || (item.filename ? item.filename.replace(/\.save$/i, '') : 'Player Save'),
                    datetime: item.datetime,
                }))
                .sort((a, b) => {
                    const aTime = a?.datetime ? new Date(a.datetime).getTime() : 0;
                    const bTime = b?.datetime ? new Date(b.datetime).getTime() : 0;
                    return bTime - aTime;
                });
            const latestTime = saves.length ? (saves[0].datetime || '') : '';
            return {
                steamId,
                playerName,
                saves,
                latestTime,
            };
        }).sort((a, b) => {
            const aTime = a?.latestTime ? new Date(a.latestTime).getTime() : 0;
            const bTime = b?.latestTime ? new Date(b.latestTime).getTime() : 0;
            if (bTime !== aTime) {
                return bTime - aTime;
            }
            return (a.playerName || '').localeCompare(b.playerName || '');
        });
    }

    function buildPlayerSaveItem(group, save) {
        const item = document.createElement('div');
        item.className = 'player-save-item';

        const body = document.createElement('div');
        body.className = 'player-save-item-body';
        const labelEl = document.createElement('p');
        labelEl.className = 'player-save-item-label';
        labelEl.textContent = save.label || 'Player Save';
        body.appendChild(labelEl);
        const metaEl = document.createElement('p');
        metaEl.className = 'player-save-item-meta';
        metaEl.textContent = formatSaveDate(save.datetime);
        body.appendChild(metaEl);
        item.appendChild(body);

        const actions = document.createElement('div');
        actions.className = 'player-save-item-actions';
        const loadBtn = document.createElement('button');
        loadBtn.className = 'btn btn-sm btn-secondary btn-load-save';
        loadBtn.dataset.saveFilename = save.filename;
        loadBtn.dataset.saveType = 'player';
        loadBtn.dataset.saveLabel = save.label || 'Player Save';
        loadBtn.innerHTML = '<i data-feather="download"></i> Load';
        actions.appendChild(loadBtn);
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'btn btn-sm btn-danger btn-delete-save';
        deleteBtn.dataset.saveFilename = save.filename;
        deleteBtn.dataset.saveType = 'player';
        deleteBtn.dataset.saveLabel = save.label || 'Player Save';
        deleteBtn.innerHTML = '<i data-feather="trash-2"></i> Delete';
        actions.appendChild(deleteBtn);
        item.appendChild(actions);
        return item;
    }

    function renderPlayerSaveGroups(groups) {
        if (!savesList) {
            return;
        }
        savesList.classList.add('player-groups-view');
        savesList.innerHTML = '';
        if (!Array.isArray(groups) || groups.length === 0) {
            if (savesEmpty) {
                savesEmpty.classList.remove('hidden');
            }
            activePlayerSaveGroupKey = null;
            return;
        }
        if (savesEmpty) {
            savesEmpty.classList.add('hidden');
        }
        ensureActivePlayerSaveGroupKey(groups);
        const fragment = document.createDocumentFragment();
        groups.forEach((group, index) => {
            const groupEl = document.createElement('section');
            groupEl.className = 'player-save-group';
            if (group.steamId) {
                groupEl.dataset.steamId = group.steamId;
            }
            const groupKey = getPlayerSaveGroupKey(group, index);
            groupEl.dataset.groupKey = groupKey;
            const isActive = !!activePlayerSaveGroupKey && groupKey === activePlayerSaveGroupKey;

            const header = document.createElement('div');
            header.className = 'player-save-header';

            const toggleBtn = document.createElement('button');
            toggleBtn.type = 'button';
            toggleBtn.className = 'player-save-toggle';
            toggleBtn.setAttribute('aria-expanded', isActive ? 'true' : 'false');
            toggleBtn.setAttribute('aria-label', 'Expand player saves');
            toggleBtn.innerHTML = '<i data-feather="chevron-right"></i>';
            header.appendChild(toggleBtn);

            const heading = document.createElement('div');
            heading.className = 'player-save-heading';
            const nameEl = document.createElement('p');
            nameEl.className = 'player-save-player';
            nameEl.textContent = group.playerName || 'Unknown Player';
            heading.appendChild(nameEl);
            const idEl = document.createElement('p');
            idEl.className = 'player-save-steamid';
            idEl.textContent = group.steamId || 'Steam ID unavailable';
            heading.appendChild(idEl);

            const state = getPlayerSaveState(group.steamId);
            if (state.state && state.state !== 'enabled') {
                const statePill = document.createElement('span');
                const modifier = state.state === 'excluded' ? 'is-save-disabled' : 'is-save-disabled';
                statePill.className = `player-save-state ${modifier}`;
                statePill.title = state.title || '';
                statePill.textContent = state.label;
                heading.appendChild(statePill);
            }

            header.appendChild(heading);

            const actionWrap = document.createElement('div');
            actionWrap.className = 'player-save-group-actions';
            if (playerSavesEnabled && group.steamId) {
                const excludeBtn = document.createElement('button');
                excludeBtn.className = 'btn btn-sm btn-danger btn-exclude-player-save';
                excludeBtn.dataset.steamId = group.steamId;
                excludeBtn.dataset.playerName = group.playerName || group.steamId;
                excludeBtn.innerHTML = '<i data-feather="slash"></i> Exclude Player';
                if (playerSaveExcludes.has(group.steamId)) {
                    excludeBtn.disabled = true;
                    excludeBtn.title = 'Player already excluded from saves';
                }
                actionWrap.appendChild(excludeBtn);
            }
            header.appendChild(actionWrap);
            groupEl.appendChild(header);

            if (playerSaveExcludes.has(group.steamId)) {
                const warning = document.createElement('div');
                warning.className = 'player-save-warning';
                warning.innerHTML = '<i data-feather="alert-triangle"></i><span>This player is excluded from automatic saves.</span>';
                groupEl.appendChild(warning);
            }

            const list = document.createElement('div');
            list.className = 'player-save-list';
            if (Array.isArray(group.saves) && group.saves.length) {
                group.saves.forEach((save) => {
                    if (!save || !save.filename) {
                        return;
                    }
                    list.appendChild(buildPlayerSaveItem(group, save));
                });
            } else {
                const emptyState = document.createElement('p');
                emptyState.className = 'player-save-empty';
                emptyState.textContent = 'No player saves available.';
                list.appendChild(emptyState);
            }
            groupEl.appendChild(list);
            if (isActive) {
                groupEl.classList.add('is-active');
            }
            const selectGroup = (event, fromToggle = false) => {
                if (event) {
                    event.preventDefault();
                    if (!fromToggle && event.target.closest('.btn-exclude-player-save')) {
                        return;
                    }
                }
                const nextKey = activePlayerSaveGroupKey === groupKey ? null : groupKey;
                setActivePlayerSaveGroup(nextKey);
            };
            toggleBtn.addEventListener('click', (event) => {
                event.stopPropagation();
                selectGroup(event, true);
            });
            header.addEventListener('click', (event) => {
                if (event.target.closest('.player-save-toggle') || event.target.closest('.btn-exclude-player-save')) {
                    return;
                }
                selectGroup(event);
            });
            fragment.appendChild(groupEl);
        });
        savesList.appendChild(fragment);
        refreshFeatherIcons();
        setActivePlayerSaveGroup(activePlayerSaveGroupKey);
    }

    async function fetchSaves(filter = 'all') {
        if (!savesList) return;
        currentSavesFilter = filter;
        savesList.classList.toggle('player-groups-view', filter === 'player');
        savesList.innerHTML = '<div class="text-muted">Loading saves...</div>';
        if (savesEmpty) {
            savesEmpty.classList.add('hidden');
        }

        if (filter === 'player') {
            try {
                const data = await serverRequest(`/saves${buildQuery({ type: 'player' })}`, { method: 'GET' });
                const groups = normalizePlayerSaveGroups(data);
                renderPlayerSaveGroups(groups);
            } catch (error) {
                handleActionError('Fetch Player Saves', error);
                if (savesList) {
                    savesList.innerHTML = '';
                }
            }
            return;
        }

        const types = SAVE_FILTER_MAP[filter] || ['auto'];
        try {
            const responses = await Promise.all(types.map(async (type) => {
                return serverRequest(`/saves${buildQuery({ type })}`, { method: 'GET' })
                    .then(data => normalizeSaveItems(type === 'manual' ? 'manual' : type, data))
                    .catch(error => {
                        handleActionError('Fetch Saves', error);
                        return [];
                    });
            }));
            const combined = responses.flat().sort((a, b) => {
                const aTime = a && a.datetime ? new Date(a.datetime).getTime() : 0;
                const bTime = b && b.datetime ? new Date(b.datetime).getTime() : 0;
                return bTime - aTime;
            });
            updateSavesList(combined);
        } catch (error) {
            handleActionError('Fetch Saves', error);
            if (savesList) {
                savesList.innerHTML = '';
            }
        }
    }

    function renameServerPrompt(currentName) {
        if (!window.SDSM || !window.SDSM.modal) {
            console.error('Modal script not loaded');
            return;
        }

        window.SDSM.modal.prompt({
            title: 'Rename Server',
            label: 'New Server Name',
            defaultValue: currentName,
            confirmText: 'Rename',
            validate: (value) => {
                if (!value || value.trim().length === 0) {
                    return 'Server name cannot be empty.';
                }
                if (value.length > 50) {
                    return 'Server name is too long.';
                }
                return true;
            }
        }).then(async (newName) => {
            const trimmed = (newName || '').trim();
            if (!trimmed) {
                return;
            }
            try {
                await serverRequest('/rename', { method: 'POST', body: { name: trimmed } });
                serverContent.dataset.serverName = trimmed;
                updateLatestSaveSummary();
            } catch (error) {
                handleActionError('Rename', error);
            }
        });
    }
    if (btnRenameServer) {
        btnRenameServer.addEventListener('click', () => renameServerPrompt(serverName));
    }

    if (languageSelect) {
        languageSelect.addEventListener('change', async (event) => {
            const selected = (event.target.value || '').trim();
            if (!selected) {
                return;
            }
            const previous = languageSelect.value;
            languageSelect.disabled = true;
            try {
                await serverRequest('/language', { method: 'POST', body: { language: selected } });
                serverContent.dataset.serverLanguage = selected;
            } catch (error) {
                languageSelect.value = previous;
                handleActionError('Language', error);
            } finally {
                languageSelect.disabled = false;
            }
        });
    }

    const controlActions = [
        { button: btnStart, endpoint: '/start', label: 'Start' },
        { button: btnStop, endpoint: '/stop', label: 'Stop' },
        { button: btnPause, endpoint: '/pause', label: 'Pause' },
        { button: btnResume, endpoint: '/resume', label: 'Resume' },
        { button: btnSave, endpoint: '/save', label: 'Save' },
        { button: btnQuickSave, endpoint: '/quicksave', label: 'Quick Save' },
        { button: btnUpdate, endpoint: '/update-server', label: 'Update' },
        { button: btnReinstall, endpoint: '/reinstall', label: 'Reinstall' },
    ];

    controlActions.forEach(({ button, endpoint, label }) => {
        if (!button) return;
        button.addEventListener('click', async () => {
            try {
                await serverRequest(endpoint, { method: 'POST' });
                fetchLatestStatus();
            } catch (error) {
                handleActionError(label || 'Action', error);
            }
        });
    });

    async function toggleStorm(start) {
        const button = start ? btnStartStorm : btnStopStorm;
        if (button) {
            button.disabled = true;
        }
        try {
            await serverRequest('/storm', { method: 'POST', body: { start: !!start } });
            await fetchLatestStatus();
        } catch (error) {
            handleActionError('Storm', error);
        } finally {
            if (button) {
                button.disabled = false;
            }
        }
    }

    if (btnStartStorm) {
        btnStartStorm.addEventListener('click', () => toggleStorm(true));
    }
    if (btnStopStorm) {
        btnStopStorm.addEventListener('click', () => toggleStorm(false));
    }

    cleanupButtons.forEach((button) => {
        button.addEventListener('click', async () => {
            const scope = button.dataset.cleanupScope;
            if (!scope) {
                return;
            }
            button.disabled = true;
            try {
                await serverRequest('/cleanup', { method: 'POST', body: { scope } });
                fetchLatestStatus();
            } catch (error) {
                handleActionError('Cleanup', error);
            } finally {
                button.disabled = false;
            }
        });
    });

    if (consoleForm) {
        consoleForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            const command = (consoleInput?.value || '').trim();
            if (!command) {
                return;
            }
            const original = consoleSubmit ? consoleSubmit.innerHTML : '';
            const wasDisabled = consoleSubmit ? consoleSubmit.disabled : false;
            if (consoleSubmit) {
                consoleSubmit.disabled = true;
                consoleSubmit.innerHTML = '<span>Sending…</span>';
            }
            try {
                await serverRequest('/console', { method: 'POST', body: { command } });
                if (consoleInput) {
                    consoleInput.value = '';
                }
            } catch (error) {
                handleActionError('Console', error);
            } finally {
                if (consoleSubmit) {
                    consoleSubmit.disabled = wasDisabled;
                    consoleSubmit.innerHTML = original || '<span>Send</span>';
                }
            }
        });
    }

    if (logsButton) {
        logsButton.addEventListener('click', () => {
            const logsCard = document.getElementById('server-logs-card');
            if (!logsCard) {
                return;
            }
            if (serverLogsToggle && serverLogsToggle.getAttribute('aria-expanded') === 'false') {
                serverLogsToggle.click();
            }
            logsCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
        });
    }

    if (btnDownloadWorld) {
        btnDownloadWorld.addEventListener('click', async () => {
            const downloadUrl = buildWorldDownloadUrl();
            const selectedName = worldDownloadSelect && !worldDownloadSelect.disabled ? (worldDownloadSelect.value || '').trim() : '';
            if (hasApiDownloadHelper) {
                try {
                    await SDSM.api.downloadWorld(serverId, selectedName, { serverName });
                } catch (error) {
                    handleActionError('Download World', error);
                }
                return;
            }
            if (hasApiHelper && SDSM.api && typeof SDSM.api.download === 'function') {
                try {
                    const options = selectedName ? { filename: selectedName } : {};
                    await SDSM.api.download(downloadUrl, options);
                } catch (error) {
                    handleActionError('Download World', error);
                }
                return;
            }
            window.location.href = downloadUrl;
        });
    }

    if (chatForm) {
        chatForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            const message = (chatInput?.value || '').trim();
            if (!message) {
                return;
            }
            try {
                await serverRequest('/chat', { method: 'POST', body: { message } });
                if (chatInput) {
                    chatInput.value = '';
                }
            } catch (error) {
                handleActionError('Chat', error);
            }
        });
    }

    if (savesTabs) {
        savesTabs.addEventListener('click', (e) => {
            const tab = e.target.closest('.tab');
            if (tab) {
                savesTabs.querySelector('.active').classList.remove('active');
                tab.classList.add('active');
                const filter = tab.dataset.saveFilter || 'all';
                fetchSaves(filter);
            }
        });
    }

    function resolveSteamIdFromElement(element) {
        if (!element || !element.dataset) {
            return '';
        }
        if (element.dataset.steamId) {
            return element.dataset.steamId;
        }
        if (element.dataset.guid) {
            return element.dataset.guid;
        }
        const row = element.closest('[data-steam-id], [data-guid]');
        if (row && row.dataset) {
            return row.dataset.steamId || row.dataset.guid || '';
        }
        return '';
    }

    document.body.addEventListener('click', (e) => {
        const kickBtn = e.target.closest('.btn-kick');
        if (kickBtn) {
            const guid = resolveSteamIdFromElement(kickBtn);
            if (guid) {
                serverRequest('/kick', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Kick', err));
            } else {
                handleActionError('Kick', new Error('Steam ID required'));
            }
            return;
        }
        const banBtn = e.target.closest('.btn-ban');
        if (banBtn) {
            const guid = resolveSteamIdFromElement(banBtn);
            if (guid) {
                serverRequest('/ban', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Ban', err));
            }
            return;
        }
        const unbanBtn = e.target.closest('.btn-unban');
        if (unbanBtn) {
            const guid = resolveSteamIdFromElement(unbanBtn);
            if (guid) {
                serverRequest('/unban', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Unban', err));
            }
            return;
        }
        const loadSaveBtn = e.target.closest('.btn-load-save');
        if (loadSaveBtn) {
            const saveName = loadSaveBtn.dataset.saveLabel || loadSaveBtn.dataset.saveFilename;
            const filename = loadSaveBtn.dataset.saveFilename;
            const type = loadSaveBtn.dataset.saveType || 'manual';
            if (filename && confirm(`Load save "${saveName}"? The server will stop before loading.`)) {
                serverRequest('/load', { method: 'POST', body: { type, name: filename } })
                    .then(() => fetchSaves(currentSavesFilter))
                    .catch(err => handleActionError('Load Save', err));
            }
            return;
        }
        const deleteSaveBtn = e.target.closest('.btn-delete-save');
        if (deleteSaveBtn) {
            const saveName = deleteSaveBtn.dataset.saveLabel || deleteSaveBtn.dataset.saveFilename;
            const filename = deleteSaveBtn.dataset.saveFilename;
            const type = deleteSaveBtn.dataset.saveType || 'manual';
            if (filename && confirm(`Delete save "${saveName}"? This cannot be undone.`)) {
                serverRequest(`/saves${buildQuery({ type, name: filename })}`, { method: 'DELETE' })
                    .then(() => fetchSaves(currentSavesFilter))
                    .catch(err => handleActionError('Delete Save', err));
            }
            return;
        }
        const excludePlayerBtn = e.target.closest('.btn-exclude-player-save');
        if (excludePlayerBtn) {
            if (!playerSavesEnabled) {
                handleActionError('Exclude Player', new Error('Player saves are disabled.'));
                return;
            }
            const steamId = excludePlayerBtn.dataset.steamId;
            const playerName = excludePlayerBtn.dataset.playerName || steamId || 'this player';
            if (!steamId) {
                handleActionError('Exclude Player', new Error('Steam ID missing.'));
                return;
            }
            const warning = `Exclude ${playerName} from player saves?\n\nThis will immediately delete all of their saved data and block future saves until re-enabled.`;
            if (!confirm(warning)) {
                return;
            }
            excludePlayerBtn.disabled = true;
            serverRequest('/player-saves/exclude', { method: 'POST', body: { steam_id: steamId } })
                .then(() => {
                    playerSaveExcludes.add(steamId);
                    syncPlayerSaveExcludeDataset();
                    fetchSaves('player');
                })
                .catch((err) => handleActionError('Exclude Player', err))
                .finally(() => {
                    excludePlayerBtn.disabled = false;
                });
            return;
        }
    });

    // Player list tabs
    const playerTabsContainer = document.querySelector('.card-players .tabs');
    if (playerTabsContainer) {
        playerTabsContainer.addEventListener('click', (e) => {
            const tab = e.target.closest('.tab');
            if (tab) {
                playerTabsContainer.querySelectorAll('.tab').forEach(t => {
                    t.classList.remove('active');
                    t.setAttribute('aria-selected', 'false');
                    const panel = document.getElementById(t.getAttribute('aria-controls'));
                    if(panel) panel.classList.add('hidden');
                });
                tab.classList.add('active');
                tab.setAttribute('aria-selected', 'true');
                const panel = document.getElementById(tab.getAttribute('aria-controls'));
                if(panel) panel.classList.remove('hidden');
            }
        });
    }


    function setupCollapsible(toggle, content) {
        if (!toggle || !content) return;
        toggle.addEventListener('click', () => {
            const isHidden = content.classList.toggle('hidden');
            toggle.setAttribute('aria-expanded', !isHidden);
            toggle.classList.toggle('active', !isHidden);
        });
    }

    setupCollapsible(serverConfigToggle, serverConfigContent);
    setupCollapsible(serverLogsToggle, serverLogsContent);

    initializeConfigControls();

    function initializeConfigControls() {
        if (!serverConfigForm || !configVersionSelect || !configWorldSelect) {
            return;
        }
        syncConfigControlDatasets();
        applyConfigVersionState();
        configVersionSelect.addEventListener('change', () => {
            applyConfigVersionState();
        });
        configWorldSelect.addEventListener('change', () => {
            configWorldSelect.dataset.currentWorld = configWorldSelect.value || '';
            if (serverConfigCard) {
                serverConfigCard.dataset.currentWorld = configWorldSelect.dataset.currentWorld;
            }
            populateWorldDetails(getVersionKeyFromSelect(configVersionSelect), configWorldSelect.value);
        });
        if (configStartLocationSelect) {
            configStartLocationSelect.addEventListener('change', () => {
                configStartLocationSelect.dataset.currentValue = configStartLocationSelect.value || '';
            });
        }
        if (configStartConditionSelect) {
            configStartConditionSelect.addEventListener('change', () => {
                configStartConditionSelect.dataset.currentValue = configStartConditionSelect.value || '';
            });
        }
        if (configDifficultySelect) {
            configDifficultySelect.addEventListener('change', () => {
                configDifficultySelect.dataset.currentValue = configDifficultySelect.value || '';
            });
        }
    }

    function syncConfigControlDatasets() {
        if (configWorldSelect) {
            configWorldSelect.dataset.currentWorld = configWorldSelect.dataset.currentWorld || configWorldSelect.value || serverConfigCard?.dataset?.currentWorld || '';
        }
        if (configStartLocationSelect) {
            configStartLocationSelect.dataset.currentValue = configStartLocationSelect.dataset.currentValue || configStartLocationSelect.value || '';
        }
        if (configStartConditionSelect) {
            configStartConditionSelect.dataset.currentValue = configStartConditionSelect.dataset.currentValue || configStartConditionSelect.value || '';
        }
        if (configDifficultySelect) {
            configDifficultySelect.dataset.currentValue = configDifficultySelect.dataset.currentValue || configDifficultySelect.value || '';
        }
    }

    function applyConfigVersionState() {
        if (!configVersionSelect || !configWorldSelect) {
            return;
        }
        const versionKey = getVersionKeyFromSelect(configVersionSelect);
        if (serverConfigCard) {
            serverConfigCard.dataset.currentVersion = versionKey;
        }
        const preferredWorld = configWorldSelect.dataset.currentWorld || configWorldSelect.value || '';
        populateWorldOptions(versionKey, preferredWorld);
        populateWorldDetails(versionKey, configWorldSelect.value);
        populateDifficultyOptions(versionKey);
    }

    function populateWorldOptions(versionKey, preferredWorld) {
        if (!configWorldSelect) {
            return;
        }
        const worlds = Array.isArray(configWorldsData[versionKey]) ? configWorldsData[versionKey] : [];
        configWorldSelect.innerHTML = '';
        const placeholder = document.createElement('option');
        placeholder.value = '';
        placeholder.textContent = worlds.length ? 'Select a world' : 'No worlds available';
        configWorldSelect.appendChild(placeholder);
        worlds.forEach((world) => {
            if (!world) {
                return;
            }
            const option = document.createElement('option');
            const value = world.id || world.ID || '';
            option.value = value;
            option.textContent = world.name || world.Name || value || 'World';
            configWorldSelect.appendChild(option);
        });
        let nextValue = preferredWorld;
        if (!nextValue || !Array.from(configWorldSelect.options).some((opt) => opt.value === nextValue)) {
            nextValue = worlds.length ? (worlds[0].id || worlds[0].ID || '') : '';
        }
        configWorldSelect.value = nextValue || '';
        configWorldSelect.disabled = worlds.length === 0;
        configWorldSelect.dataset.currentWorld = configWorldSelect.value || '';
        if (serverConfigCard) {
            serverConfigCard.dataset.currentWorld = configWorldSelect.dataset.currentWorld;
        }
    }

    function populateWorldDetails(versionKey, worldId) {
        const versionBundle = configWorldMeta?.[versionKey] || {};
        const entry = worldId ? versionBundle[worldId] : null;
        const locations = entry && Array.isArray(entry.locations) ? entry.locations : [];
        const conditions = entry && Array.isArray(entry.conditions) ? entry.conditions : [];
        populateSelectOptions(configStartLocationSelect, locations, configStartLocationSelect?.dataset?.currentValue, { valueKey: 'ID', labelKey: 'Name', emptyLabel: 'Select a world first' });
        populateSelectOptions(configStartConditionSelect, conditions, configStartConditionSelect?.dataset?.currentValue, { valueKey: 'ID', labelKey: 'Name', emptyLabel: 'Select a world first' });
    }

    function populateSelectOptions(select, items, preferredValue, { valueKey = 'value', labelKey = 'label', emptyLabel = 'Select an option' } = {}) {
        if (!select) {
            return;
        }
        select.innerHTML = '';
        if (!Array.isArray(items) || !items.length) {
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = emptyLabel;
            select.appendChild(placeholder);
            select.value = '';
            select.disabled = true;
            select.dataset.currentValue = '';
            return;
        }
        select.disabled = false;
        items.forEach((item) => {
            if (!item) {
                return;
            }
            const option = document.createElement('option');
            const normalizedValueKey = typeof valueKey === 'string' ? valueKey : 'value';
            const normalizedLabelKey = typeof labelKey === 'string' ? labelKey : 'label';
            const lowerValueKey = normalizedValueKey.toLowerCase();
            const lowerLabelKey = normalizedLabelKey.toLowerCase();
            const value = item[normalizedValueKey] ?? item[lowerValueKey] ?? item.id ?? item.ID ?? '';
            const label = item[normalizedLabelKey] ?? item[lowerLabelKey] ?? (value || 'Option');
            option.value = value;
            option.textContent = label;
            if (item.Description || item.description) {
                option.title = item.Description || item.description;
            }
            select.appendChild(option);
        });
        let nextValue = preferredValue;
        if (!nextValue || !Array.from(select.options).some((opt) => opt.value === nextValue)) {
            nextValue = select.dataset.currentValue || '';
        }
        if (!nextValue || !Array.from(select.options).some((opt) => opt.value === nextValue)) {
            nextValue = select.options[0]?.value || '';
        }
        select.value = nextValue;
        select.dataset.currentValue = select.value || '';
    }

    function populateDifficultyOptions(versionKey) {
        if (!configDifficultySelect) {
            return;
        }
        const difficulties = Array.isArray(configDifficulties[versionKey]) ? configDifficulties[versionKey] : [];
        configDifficultySelect.innerHTML = '';
        if (!difficulties.length) {
            const option = document.createElement('option');
            const fallback = configDifficultySelect.dataset.currentValue || configDifficultySelect.value || 'Normal';
            option.value = fallback;
            option.textContent = fallback;
            configDifficultySelect.appendChild(option);
            configDifficultySelect.disabled = true;
            configDifficultySelect.dataset.currentValue = fallback;
            return;
        }
        configDifficultySelect.disabled = false;
        difficulties.forEach((difficulty) => {
            const option = document.createElement('option');
            option.value = `${difficulty}`;
            option.textContent = `${difficulty}`;
            configDifficultySelect.appendChild(option);
        });
        let nextValue = configDifficultySelect.dataset.currentValue || configDifficultySelect.value || '';
        if (!nextValue || !Array.from(configDifficultySelect.options).some((opt) => opt.value === nextValue)) {
            nextValue = `${difficulties[0]}`;
        }
        configDifficultySelect.value = nextValue;
        configDifficultySelect.dataset.currentValue = configDifficultySelect.value || '';
    }

    function getVersionKeyFromSelect(select) {
        if (!select) {
            return 'release';
        }
        return select.value === 'true' ? 'beta' : 'release';
    }

    if (serverConfigForm) {
        const configSubmitButton = serverConfigForm.querySelector('button[type="submit"]');
        const resetButton = document.getElementById('btn-reset-config');

        if (resetButton) {
            resetButton.addEventListener('click', () => {
                serverConfigForm.reset();
                setTimeout(() => {
                    syncConfigControlDatasets();
                    applyConfigVersionState();
                }, 0);
            });
        }

        serverConfigForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            const prevDisabled = configSubmitButton ? configSubmitButton.disabled : false;
            if (configSubmitButton) {
                configSubmitButton.disabled = true;
                configSubmitButton.dataset.originalText = configSubmitButton.dataset.originalText || configSubmitButton.innerHTML;
                configSubmitButton.innerHTML = '<span>Saving...</span>';
            }
            try {
                const formData = new FormData(serverConfigForm);
                await serverRequest('/settings', { method: 'POST', body: formData });
            } catch (error) {
                handleActionError('Update Configuration', error);
            } finally {
                if (configSubmitButton) {
                    configSubmitButton.innerHTML = configSubmitButton.dataset.originalText || configSubmitButton.innerHTML;
                    configSubmitButton.disabled = prevDisabled;
                }
            }
        });
    }

    // Log viewer logic
    let activeLogFile = '';

    async function fetchLogFiles() {
        if (!logTabs) return;
        logTabs.innerHTML = '';
        try {
            const data = await serverRequest('/logs', { method: 'GET' });
            const files = Array.isArray(data.files) ? data.files : [];
            if (!files.length) {
                if (logTabsEmpty) logTabsEmpty.classList.remove('hidden');
                if (logViewer) logViewer.textContent = '';
                return;
            }
            if (logTabsEmpty) logTabsEmpty.classList.add('hidden');
            files.forEach(file => {
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.className = 'log-file-btn';
                btn.dataset.logFile = file;
                btn.textContent = file;
                if (file === activeLogFile) {
                    btn.classList.add('active');
                }
                logTabs.appendChild(btn);
            });
            const targetFile = activeLogFile && files.includes(activeLogFile) ? activeLogFile : files[0];
            const targetButton = Array.from(logTabs.querySelectorAll('.log-file-btn')).find(btn => btn.dataset.logFile === targetFile);
            if (targetButton) {
                targetButton.classList.add('active');
                fetchLogContent(targetFile);
            }
        } catch (error) {
            handleActionError('Fetch Logs', error);
            if (logTabsEmpty) logTabsEmpty.classList.remove('hidden');
        }
    }

    async function fetchLogContent(logFile) {
        if (!logViewer) return;
        activeLogFile = logFile;
        logViewer.textContent = 'Loading log...';
        try {
            const response = await fetch(`${serverApiBase}/log${buildQuery({ name: logFile })}`, {
                method: 'GET',
                headers: { Accept: 'text/plain', 'HX-Request': 'true' },
                credentials: 'same-origin',
            });
            if (!response.ok) {
                throw new Error('Unable to load log file');
            }
            const text = await response.text();
            logViewer.textContent = text;
        } catch (error) {
            handleActionError('Fetch Log', error);
            if (logViewer) {
                logViewer.textContent = 'Unable to load log file.';
            }
        }
    }

    if (logTabs) {
        logTabs.addEventListener('click', (e) => {
            const btn = e.target.closest('.log-file-btn');
            if (btn) {
                logTabs.querySelectorAll('.log-file-btn').forEach(b => b.classList.remove('active'));
                btn.classList.add('active');
                logTabs.querySelectorAll('.log-file-btn').forEach(b => b.classList.remove('active'));
                btn.classList.add('active');
                const logFile = btn.dataset.logFile;
                fetchLogContent(logFile);
            }
        });
    }

    if(slRefresh) slRefresh.addEventListener('click', fetchLogFiles);
    if (slClear) {
        slClear.addEventListener('click', () => {
            if (confirm('Are you sure you want to clear all server logs?')) {
                if (!activeLogFile) {
                    handleActionError('Clear Log', new Error('Select a log file to clear.'));
                    return;
                }
                serverRequest('/log/clear', { method: 'POST', body: { name: activeLogFile } })
                    .then(fetchLogFiles)
                    .catch(err => handleActionError('Clear Log', err));
            }
        });
    }
    if(slDownload) {
        slDownload.addEventListener('click', () => {
            const activeLog = logTabs ? logTabs.querySelector('.log-file-btn.active') : null;
            if (activeLog) {
                window.location.href = `${serverApiBase}/log/download${buildQuery({ name: activeLog.dataset.logFile })}`;
            } else {
                showToast('Info', 'Please select a log file to download.', 'info');
            }
        });
    }

    // Initial load
    applyInitialStatusFromDataset();
    fetchLatestStatus();
    startStatusRefreshLoop();
    fetchSaves('all');
    
    if (logViewer) {
        fetchLogFiles();
    }

    if (typeof window.WebSocket !== 'undefined') {
        connectWebSocket();
    }

    window.addEventListener('beforeunload', () => {
        if (statusRefreshTimer) {
            clearInterval(statusRefreshTimer);
        }
        if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
            try {
                socket.close(1000, 'page unload');
            } catch (_) {
                // ignore
            }
        }
    });
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initServerStatusDashboard, { once: true });
} else {
    initServerStatusDashboard();
}
