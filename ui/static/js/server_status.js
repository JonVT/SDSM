document.addEventListener('DOMContentLoaded', function () {
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

    const chatEmpty = document.getElementById('chat-empty');

    const savesList = document.getElementById('saves-list');
    const savesEmpty = document.getElementById('saves-empty');
    const savesTabs = document.getElementById('saves-tabs');
    let currentSavesFilter = 'all';
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
    const bannedPlayersTable = document.getElementById('banned-players-table');
    const bannedPlayersEmpty = document.getElementById('banned-players-empty');
    const bannedCount = document.getElementById('banned-count');

    const serverConfigForm = document.getElementById('server-config-form');
    const serverConfigContent = document.getElementById('serverConfigContent');
    const serverConfigToggle = document.getElementById('serverConfigToggle');
    const serverLogsContent = document.getElementById('serverLogsContent');
    const serverLogsToggle = document.getElementById('serverLogsToggle');

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
            } else {
                showToast('Error', 'Could not reconnect to server. Please refresh the page.', 'danger');
            }
        };

        socket.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    function handleWebSocketMessage(data) {
        switch (data.type) {
            case 'status':
                updateServerStatus(data.payload);
                break;
            case 'chat':
                addChatMessage(data.payload);
                break;
            case 'players':
                updatePlayerLists(data.payload);
                break;
            case 'log':
                // This is handled by the log viewer component, but we might want to tail logs
                break;
            case 'error':
                showToast('Error', data.payload.message, 'danger');
                break;
            case 'saves':
                fetchSaves(currentSavesFilter);
                break;
        }
    }

    function updateServerStatus(status) {
        serverContent.dataset.serverRunning = status.running;
        serverContent.dataset.serverPaused = status.paused;
        serverContent.dataset.serverStarting = status.starting;
        serverContent.dataset.serverStopping = status.stopping;

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
        
        const configSubmitButton = serverConfigForm ? serverConfigForm.querySelector('button[type="submit"]') : null;
        if(configSubmitButton) {
            configSubmitButton.disabled = status.running;
        }
    }

    function addChatMessage(message) {
        if (chatEmpty) {
            chatEmpty.classList.add('hidden');
        }
        if (!chatLog) return;

        const msgElement = document.createElement('div');
        msgElement.classList.add('chat-message');
        
        const timestamp = new Date(message.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

        msgElement.innerHTML = `
            <span class="chat-timestamp">${timestamp}</span>
            <strong class="chat-author">${message.author}:</strong>
            <span class="chat-text">${message.text}</span>
        `;
        chatLog.appendChild(msgElement);
        chatLog.scrollTop = chatLog.scrollHeight;
    }

    function updatePlayerTable(tableBody, emptyState, players, rowTemplate) {
        if (!tableBody) return;
        tableBody.innerHTML = '';
        if (players && players.length > 0) {
            players.forEach(player => {
                const rowHTML = rowTemplate(player);
                const row = htmx.parseDOM(rowHTML)[0];
                tableBody.appendChild(row);
            });
            if (emptyState) emptyState.classList.add('hidden');
        } else {
            if (emptyState) emptyState.classList.remove('hidden');
        }
    }
    
    function updatePlayerLists(players) {
        updatePlayerTable(livePlayersTable, livePlayersEmpty, players.live, (p) => {
            const template = document.getElementById('live-player-row-template');
            if (!template) return '';
            let content = template.innerHTML;
            return content.replace(/{{.GUID}}/g, p.GUID).replace('{{.Name}}', p.Name).replace('{{.IP}}', p.IP).replace('{{.Connected}}', p.Connected);
        });
        if (liveCount) liveCount.textContent = players.live ? players.live.length : 0;

        updatePlayerTable(historyPlayersTable, historyPlayersEmpty, players.history, (p) => {
            const template = document.getElementById('history-player-row-template');
            if (!template) return '';
            let content = template.innerHTML;
            return content.replace(/{{.GUID}}/g, p.GUID).replace('{{.Name}}', p.Name).replace('{{.LastSeen}}', p.LastSeen);
        });
        if (historyCount) historyCount.textContent = players.history ? players.history.length : 0;

        if (bannedPlayersTable) {
            updatePlayerTable(bannedPlayersTable, bannedPlayersEmpty, players.banned, (p) => {
                const template = document.getElementById('banned-player-row-template');
                if (!template) return '';
                let content = template.innerHTML;
                return content.replace(/{{.GUID}}/g, p.GUID).replace('{{.Name}}', p.Name).replace('{{.BannedOn}}', p.BannedOn);
            });
            if (bannedCount) bannedCount.textContent = players.banned ? players.banned.length : 0;
        }
    }


    function updateSavesList(saves) {
        if (!savesList) return;
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

        saves.forEach(save => {
            if (!save || !save.filename) {
                return;
            }
            const date = save.datetime ? new Date(save.datetime).toLocaleString() : 'Unknown time';
            const label = save.label || save.name || save.filename.replace(/\.save$/i, '');
            const saveElement = document.createElement('div');
            saveElement.classList.add('save-item');

            const info = document.createElement('div');
            info.className = 'save-info';

            const nameEl = document.createElement('strong');
            nameEl.className = 'save-name';
            nameEl.textContent = label;

            const dateEl = document.createElement('span');
            dateEl.className = 'save-date text-muted text-sm';
            dateEl.textContent = date;

            info.appendChild(nameEl);
            info.appendChild(dateEl);

            const metaParts = [];
            if (save.typeLabel) {
                metaParts.push(save.typeLabel);
            }
            if (save.playerName || save.steamId) {
                const playerLabel = save.playerName ? `${save.playerName}${save.steamId ? ` (${save.steamId})` : ''}` : save.steamId;
                metaParts.push(`Player: ${playerLabel}`);
            }
            if (metaParts.length > 0) {
                const meta = document.createElement('div');
                meta.className = 'save-meta text-xs text-muted';
                meta.textContent = metaParts.join(' • ');
                info.appendChild(meta);
            }

            const actions = document.createElement('div');
            actions.className = 'save-actions';

            const loadBtn = document.createElement('button');
            loadBtn.className = 'btn btn-sm btn-secondary btn-load-save';
            loadBtn.dataset.saveFilename = save.filename;
            loadBtn.dataset.saveType = save.type;
            loadBtn.dataset.saveLabel = label;
            loadBtn.textContent = 'Load';
            loadBtn.title = 'Load this save';

            const deleteBtn = document.createElement('button');
            deleteBtn.className = 'btn btn-sm btn-danger btn-delete-save';
            deleteBtn.dataset.saveFilename = save.filename;
            deleteBtn.dataset.saveType = save.type;
            deleteBtn.dataset.saveLabel = label;
            deleteBtn.textContent = 'Delete';
            deleteBtn.title = 'Delete this save';

            actions.appendChild(loadBtn);
            actions.appendChild(deleteBtn);

            saveElement.appendChild(info);
            saveElement.appendChild(actions);
            savesList.appendChild(saveElement);
        });
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

    async function fetchSaves(filter = 'all') {
        if (!savesList) return;
        currentSavesFilter = filter;
        savesList.innerHTML = '<div class="text-muted">Loading saves...</div>';
        if (savesEmpty) {
            savesEmpty.classList.add('hidden');
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
            } catch (error) {
                handleActionError('Rename', error);
            }
        });
    }
    if (btnRenameServer) {
        btnRenameServer.addEventListener('click', () => renameServerPrompt(serverName));
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
            } catch (error) {
                handleActionError(label || 'Action', error);
            }
        });
    });

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

    document.body.addEventListener('click', (e) => {
        const kickBtn = e.target.closest('.btn-kick');
        if (kickBtn) {
            const guid = kickBtn.dataset.guid;
            serverRequest('/kick', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Kick', err));
            return;
        }
        const banBtn = e.target.closest('.btn-ban');
        if (banBtn) {
            const guid = banBtn.dataset.guid;
            serverRequest('/ban', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Ban', err));
            return;
        }
        const unbanBtn = e.target.closest('.btn-unban');
        if (unbanBtn) {
            const guid = unbanBtn.dataset.guid;
            serverRequest('/unban', { method: 'POST', body: { steam_id: guid } }).catch(err => handleActionError('Unban', err));
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

    if (serverConfigForm) {
        const configSubmitButton = serverConfigForm.querySelector('button[type="submit"]');
        const resetButton = document.getElementById('btn-reset-config');

        if (resetButton) {
            resetButton.addEventListener('click', () => {
                serverConfigForm.reset();
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
    connectWebSocket();
    fetchSaves('all');
    
    if (logViewer) {
        fetchLogFiles();
    }
});
