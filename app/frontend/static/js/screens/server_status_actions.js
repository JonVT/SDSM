(function(window) {
    'use strict';

    if (window.SDSMServerStatusActions) {
        return;
    }

    function bind(ctx) {
        const on = ctx.on;
        const els = ctx.elements;

        function requestDeleteConfirmation(serverName, isRunning) {
            const normalizedName = String(serverName || '').trim() || 'this server';
            const intro = isRunning
                ? `Delete server "${normalizedName}"? The server will be stopped, removed from SDSM, and its files will be deleted.`
                : `Delete server "${normalizedName}"? This will remove it from SDSM and delete its files.`;
            const hint = `${intro} This cannot be undone.`;

            if (window.SDSM && SDSM.modal && typeof SDSM.modal.prompt === 'function') {
                return SDSM.modal.prompt({
                    title: 'Delete Server',
                    label: `Type "${normalizedName}" to confirm deletion`,
                    placeholder: normalizedName,
                    defaultValue: '',
                    confirmText: 'Delete Server',
                    cancelText: 'Cancel',
                    hint,
                    danger: true,
                    validate: (value) => {
                        if (!value) {
                            return 'Enter the server name to confirm deletion.';
                        }
                        if (value.trim() !== normalizedName) {
                            return `Enter "${normalizedName}" exactly to confirm.`;
                        }
                        return true;
                    }
                });
            }

            return Promise.resolve(window.prompt(
                `${hint}\n\nTo confirm, type the server name exactly as shown:\n${normalizedName}`,
                ''
            ));
        }

        const controlActions = [
            { button: els.btnStart, endpoint: '/start', label: 'Start' },
            { button: els.btnStop, endpoint: '/stop', label: 'Stop' },
            { button: els.btnRestart, endpoint: '/restart', label: 'Restart' },
            { button: els.btnSave, endpoint: '/save', label: 'Save' },
            { button: els.btnQuickSave, endpoint: '/quicksave', label: 'Quick Save' },
            { button: els.btnUpdate, endpoint: '/update-server', label: 'Update' },
            { button: els.btnReinstall, endpoint: '/reinstall', label: 'Reinstall' },
        ];

        controlActions.forEach(({ button, endpoint, label }) => {
            if (!button) return;
            on(button, 'click', async () => {
                try {
                    await ctx.serverRequest(endpoint, { method: 'POST' });
                    await ctx.fetchLatestStatus();
                } catch (error) {
                    ctx.handleActionError(label || 'Action', error);
                }
            });
        });

        if (els.btnDeleteServer) {
            on(els.btnDeleteServer, 'click', async () => {
                const isRunning = ctx.serverContent && ctx.serverContent.dataset.serverRunning === 'true';
                const serverName = String(ctx.serverName || '').trim() || 'this server';
                const typedName = await requestDeleteConfirmation(serverName, isRunning);

                if (typedName === null) {
                    return;
                }

                if (typedName.trim() !== serverName) {
                    const mismatchMessage = `Deletion cancelled. Enter "${serverName}" exactly to confirm.`;
                    if (window.showToast) {
                        window.showToast('Delete Cancelled', mismatchMessage, 'warning');
                    }
                    return;
                }

                const original = els.btnDeleteServer.innerHTML;
                els.btnDeleteServer.disabled = true;
                els.btnDeleteServer.innerHTML = '<i data-feather="loader" class="btn-icon-left"></i> Deleting…';
                if (window.feather && typeof window.feather.replace === 'function') {
                    window.feather.replace();
                }

                try {
                    await ctx.serverRequest('/delete', { method: 'POST' });
                    window.location.assign('/dashboard');
                } catch (error) {
                    els.btnDeleteServer.disabled = false;
                    els.btnDeleteServer.innerHTML = original;
                    if (window.feather && typeof window.feather.replace === 'function') {
                        window.feather.replace();
                    }
                    ctx.handleActionError('Delete Server', error);
                }
            });
        }

        if (els.btnPause) {
            on(els.btnPause, 'click', async () => {
                const isPaused = ctx.serverContent && ctx.serverContent.dataset.serverPaused === 'true';
                const endpoint = isPaused ? '/resume' : '/pause';
                const label = isPaused ? 'Resume' : 'Pause';
                try {
                    await ctx.serverRequest(endpoint, { method: 'POST' });
                    await ctx.fetchLatestStatus();
                } catch (error) {
                    ctx.handleActionError(label, error);
                }
            });
        }

        const toggleStorm = async (start) => {
            const button = start ? els.btnStartStorm : els.btnStopStorm;
            if (button) {
                button.disabled = true;
            }
            try {
                await ctx.serverRequest('/storm', { method: 'POST', body: { start: !!start } });
                await ctx.fetchLatestStatus();
            } catch (error) {
                ctx.handleActionError('Storm', error);
            } finally {
                if (button) {
                    button.disabled = false;
                }
            }
        };

        if (els.btnStartStorm) {
            on(els.btnStartStorm, 'click', () => toggleStorm(true));
        }
        if (els.btnStopStorm) {
            on(els.btnStopStorm, 'click', () => toggleStorm(false));
        }

        (els.cleanupButtons || []).forEach((button) => {
            on(button, 'click', async () => {
                const scope = button.dataset.cleanupScope;
                if (!scope) {
                    return;
                }
                button.disabled = true;
                try {
                    await ctx.serverRequest('/cleanup', { method: 'POST', body: { scope } });
                    await ctx.fetchLatestStatus();
                } catch (error) {
                    ctx.handleActionError('Cleanup', error);
                } finally {
                    button.disabled = false;
                }
            });
        });

        if (els.consoleForm) {
            on(els.consoleForm, 'submit', async (event) => {
                event.preventDefault();
                const command = (els.consoleInput?.value || '').trim();
                if (!command) {
                    return;
                }
                const original = els.consoleSubmit ? els.consoleSubmit.innerHTML : '';
                const wasDisabled = els.consoleSubmit ? els.consoleSubmit.disabled : false;
                if (els.consoleSubmit) {
                    els.consoleSubmit.disabled = true;
                    els.consoleSubmit.innerHTML = '<span>Sending…</span>';
                }
                try {
                    await ctx.serverRequest('/console', { method: 'POST', body: { command } });
                    if (els.consoleInput) {
                        els.consoleInput.value = '';
                    }
                } catch (error) {
                    ctx.handleActionError('Console', error);
                } finally {
                    if (els.consoleSubmit) {
                        els.consoleSubmit.disabled = wasDisabled;
                        els.consoleSubmit.innerHTML = original || '<span>Send</span>';
                    }
                }
            });
        }

        if (els.logsButton) {
            on(els.logsButton, 'click', () => {
                const logsCard = document.getElementById('server-logs-card');
                if (!logsCard) {
                    return;
                }
                const logsDetails = document.querySelector('[data-collapse-id="server-status-logs"]');
                if (logsDetails && !logsDetails.open) {
                    logsDetails.open = true;
                }
                logsCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
            });
        }

        if (els.btnDownloadWorld) {
            on(els.btnDownloadWorld, 'click', async () => {
                const downloadUrl = ctx.buildWorldDownloadUrl();
                const selectedName = els.worldDownloadSelect && !els.worldDownloadSelect.disabled ? (els.worldDownloadSelect.value || '').trim() : '';
                if (ctx.hasApiDownloadHelper) {
                    try {
                        await SDSM.api.downloadWorld(ctx.serverId, selectedName, { serverName: ctx.serverName });
                    } catch (error) {
                        ctx.handleActionError('Download World', error);
                    }
                    return;
                }
                if (ctx.hasApiHelper && SDSM.api && typeof SDSM.api.download === 'function') {
                    try {
                        const options = selectedName ? { filename: selectedName } : {};
                        await SDSM.api.download(downloadUrl, options);
                    } catch (error) {
                        ctx.handleActionError('Download World', error);
                    }
                    return;
                }
                window.location.href = downloadUrl;
            });
        }

        if (els.chatForm) {
            on(els.chatForm, 'submit', async (event) => {
                event.preventDefault();
                const message = (els.chatInput?.value || '').trim();
                if (!message) {
                    return;
                }
                try {
                    await ctx.serverRequest('/chat', { method: 'POST', body: { message } });
                    if (els.chatInput) {
                        els.chatInput.value = '';
                    }
                } catch (error) {
                    ctx.handleActionError('Chat', error);
                }
            });
        }

        if (els.savesTabs) {
            on(els.savesTabs, 'click', (event) => {
                const tab = event.target.closest('.tab');
                if (!tab) {
                    return;
                }
                const currentActive = els.savesTabs.querySelector('.active');
                if (currentActive) {
                    currentActive.classList.remove('active');
                }
                tab.classList.add('active');
                const filter = tab.dataset.saveFilter || 'all';
                ctx.fetchSaves(filter);
            });
        }

        on(document, 'click', async (event) => {
            const copyBtn = event.target.closest('[data-copy-value][data-copy-scope="server-info"]');
            if (!copyBtn) {
                return;
            }
            event.preventDefault();
            event.stopPropagation();
            const value = copyBtn.getAttribute('data-copy-value');
            try {
                await ctx.copyTextToClipboard(value);
                copyBtn.setAttribute('data-copied', 'true');
                if (ctx.copyTimers.has(copyBtn)) {
                    clearTimeout(ctx.copyTimers.get(copyBtn));
                }
                const timeoutId = setTimeout(() => {
                    copyBtn.removeAttribute('data-copied');
                    ctx.copyTimers.delete(copyBtn);
                }, 1500);
                ctx.copyTimers.set(copyBtn, timeoutId);
            } catch (err) {
                console.warn('Copy failed', err);
            }
        });

        on(document.body, 'click', (event) => {
            const kickBtn = event.target.closest('.btn-kick');
            if (kickBtn) {
                const guid = ctx.resolveSteamIdFromElement(kickBtn);
                if (guid) {
                    ctx.serverRequest('/kick', { method: 'POST', body: { steam_id: guid } }).catch(err => ctx.handleActionError('Kick', err));
                } else {
                    ctx.handleActionError('Kick', new Error('Steam ID required'));
                }
                return;
            }

            const banBtn = event.target.closest('.btn-ban');
            if (banBtn) {
                const guid = ctx.resolveSteamIdFromElement(banBtn);
                if (guid) {
                    ctx.serverRequest('/ban', { method: 'POST', body: { steam_id: guid } }).catch(err => ctx.handleActionError('Ban', err));
                }
                return;
            }

            const unbanBtn = event.target.closest('.btn-unban');
            if (unbanBtn) {
                const guid = ctx.resolveSteamIdFromElement(unbanBtn);
                if (guid) {
                    ctx.serverRequest('/unban', { method: 'POST', body: { steam_id: guid } }).catch(err => ctx.handleActionError('Unban', err));
                }
                return;
            }

            const loadSaveBtn = event.target.closest('.btn-load-save');
            if (loadSaveBtn) {
                const saveName = loadSaveBtn.dataset.saveLabel || loadSaveBtn.dataset.saveFilename;
                const filename = loadSaveBtn.dataset.saveFilename;
                const type = loadSaveBtn.dataset.saveType || 'manual';
                if (filename && confirm(`Load save "${saveName}"? The server will stop before loading.`)) {
                    ctx.serverRequest('/load', { method: 'POST', body: { type, name: filename } })
                        .then(() => ctx.fetchSaves(ctx.getCurrentSavesFilter()))
                        .catch(err => ctx.handleActionError('Load Save', err));
                }
                return;
            }

            const deleteSaveBtn = event.target.closest('.btn-delete-save');
            if (deleteSaveBtn) {
                const saveName = deleteSaveBtn.dataset.saveLabel || deleteSaveBtn.dataset.saveFilename;
                const filename = deleteSaveBtn.dataset.saveFilename;
                const type = deleteSaveBtn.dataset.saveType || 'manual';
                if (filename && confirm(`Delete save "${saveName}"? This cannot be undone.`)) {
                    ctx.serverRequest(`/saves${ctx.buildQuery({ type, name: filename })}`, { method: 'DELETE' })
                        .then(() => ctx.fetchSaves(ctx.getCurrentSavesFilter()))
                        .catch(err => ctx.handleActionError('Delete Save', err));
                }
                return;
            }

            const toggleSaveBtn = event.target.closest('.btn-toggle-player-save');
            if (toggleSaveBtn) {
                if (!ctx.playerSavesEnabled) {
                    return;
                }
                const steamId = toggleSaveBtn.dataset.steamId;
                const playerName = toggleSaveBtn.dataset.playerName || steamId || 'this player';
                const currentState = toggleSaveBtn.dataset.saveState;
                if (!steamId) {
                    return;
                }
                if (currentState === 'enabled') {
                    const warning = `Exclude ${playerName} from player saves?\n\nThis will immediately delete all of their saved data and block future saves until re-enabled.`;
                    if (!confirm(warning)) {
                        return;
                    }
                    toggleSaveBtn.disabled = true;
                    ctx.serverRequest('/player-saves/exclude', { method: 'POST', body: { steam_id: steamId } })
                        .then(() => {
                            ctx.playerSaveExcludes.add(steamId);
                            ctx.syncPlayerSaveExcludeDataset();
                            toggleSaveBtn.dataset.saveState = 'excluded';
                            toggleSaveBtn.className = 'btn btn-icon btn-sm btn-secondary save-off btn-toggle-player-save';
                            toggleSaveBtn.title = 'Click to enable player saves';
                            toggleSaveBtn.innerHTML = '<i data-feather="save"></i>';
                            if (typeof feather !== 'undefined') { feather.replace({ width: 14, height: 14 }); }
                            ctx.fetchSaves('player');
                        })
                        .catch((err) => ctx.handleActionError('Exclude Player', err))
                        .finally(() => { toggleSaveBtn.disabled = false; });
                } else {
                    toggleSaveBtn.disabled = true;
                    ctx.serverRequest('/player-saves/include', { method: 'POST', body: { steam_id: steamId } })
                        .then(() => {
                            ctx.playerSaveExcludes.delete(steamId);
                            ctx.syncPlayerSaveExcludeDataset();
                            toggleSaveBtn.dataset.saveState = 'enabled';
                            toggleSaveBtn.className = 'btn btn-icon btn-sm btn-success btn-toggle-player-save';
                            toggleSaveBtn.title = 'Click to disable player saves';
                            toggleSaveBtn.innerHTML = '<i data-feather="save"></i>';
                            if (typeof feather !== 'undefined') { feather.replace({ width: 14, height: 14 }); }
                        })
                        .catch((err) => ctx.handleActionError('Include Player', err))
                        .finally(() => { toggleSaveBtn.disabled = false; });
                }
                return;
            }

            const excludePlayerBtn = event.target.closest('.btn-exclude-player-save');
            if (excludePlayerBtn) {
                if (!ctx.playerSavesEnabled) {
                    ctx.handleActionError('Exclude Player', new Error('Player saves are disabled.'));
                    return;
                }
                const steamId = excludePlayerBtn.dataset.steamId;
                const playerName = excludePlayerBtn.dataset.playerName || steamId || 'this player';
                if (!steamId) {
                    ctx.handleActionError('Exclude Player', new Error('Steam ID missing.'));
                    return;
                }
                const warning = `Exclude ${playerName} from player saves?\n\nThis will immediately delete all of their saved data and block future saves until re-enabled.`;
                if (!confirm(warning)) {
                    return;
                }
                excludePlayerBtn.disabled = true;
                ctx.serverRequest('/player-saves/exclude', { method: 'POST', body: { steam_id: steamId } })
                    .then(() => {
                        ctx.playerSaveExcludes.add(steamId);
                        ctx.syncPlayerSaveExcludeDataset();
                        ctx.fetchSaves('player');
                    })
                    .catch((err) => ctx.handleActionError('Exclude Player', err))
                    .finally(() => {
                        excludePlayerBtn.disabled = false;
                    });
            }
        });

        if (els.playerTabsContainer) {
            const syncPlayerTabs = (activeTab) => {
                const tabs = Array.from(els.playerTabsContainer.querySelectorAll('.tab'));
                tabs.forEach((t) => {
                    const isActive = t === activeTab;
                    t.classList.toggle('active', isActive);
                    t.setAttribute('role', 'tab');
                    t.setAttribute('aria-selected', isActive ? 'true' : 'false');
                    t.setAttribute('tabindex', isActive ? '0' : '-1');
                    const panel = document.getElementById(t.getAttribute('aria-controls'));
                    if (panel) {
                        panel.classList.toggle('hidden', !isActive);
                        panel.setAttribute('role', 'tabpanel');
                        if (t.id) {
                            panel.setAttribute('aria-labelledby', t.id);
                        }
                    }
                });
            };

            on(els.playerTabsContainer, 'click', (event) => {
                const tab = event.target.closest('.tab');
                if (!tab) {
                    return;
                }
                syncPlayerTabs(tab);
            });

            on(els.playerTabsContainer, 'keydown', (event) => {
                const current = event.target.closest('.tab');
                if (!current) {
                    return;
                }

                const tabs = Array.from(els.playerTabsContainer.querySelectorAll('.tab'));
                const currentIndex = tabs.indexOf(current);
                if (currentIndex < 0 || tabs.length === 0) {
                    return;
                }

                let nextIndex = -1;
                switch (event.key) {
                    case 'Enter':
                    case ' ':
                        event.preventDefault();
                        syncPlayerTabs(current);
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
                if (!nextTab) {
                    return;
                }
                nextTab.focus();
                syncPlayerTabs(nextTab);
            });

            const tabs = Array.from(els.playerTabsContainer.querySelectorAll('.tab'));
            tabs.forEach((tab, index) => {
                if (!tab.id) {
                    tab.id = `server-player-tab-${index + 1}`;
                }
            });
            const active = els.playerTabsContainer.querySelector('.tab.active') || tabs[0] || null;
            if (active) {
                syncPlayerTabs(active);
            }
        }
    }

    window.SDSMServerStatusActions = {
        bind,
    };
})(window);
