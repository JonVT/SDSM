(function(window) {
    'use strict';

    if (window.SDSMServerStatusView) {
        return;
    }

    function applyStatus(ctx, status) {
        const state = ctx.state;
        const els = ctx.elements;

        const wasRunning = state.lastKnownRunning;
        const isRunning = !!status.running;
        state.lastKnownRunning = isRunning;

        let pillClass = 'is-stopped';
        let text = 'Stopped';

        if (status.lastError) {
            pillClass = 'is-error';
            text = 'Error';
        } else if (status.stopping) {
            pillClass = 'is-stopping';
            text = 'Stopping';
        } else if (status.starting) {
            pillClass = 'is-starting';
            text = 'Starting';
        } else if (status.running) {
            if (status.paused) {
                pillClass = 'is-paused';
                text = 'Paused';
            } else {
                pillClass = 'is-running';
                text = 'Running';
            }
        }

        const infoStatePill = document.querySelector('[data-server-state-pill]');
        if (infoStatePill) {
            infoStatePill.classList.remove('is-running', 'is-stopped', 'is-paused', 'is-starting', 'is-stopping', 'is-error');
            infoStatePill.classList.add(pillClass);
            infoStatePill.textContent = text;
        }

        if (els.btnStart) els.btnStart.disabled = status.running || status.starting;
        if (els.btnStop) els.btnStop.disabled = !status.running || status.stopping;
        if (els.btnRestart) els.btnRestart.disabled = !status.running || status.stopping;
        if (els.btnPause) {
            els.btnPause.disabled = !status.running || status.stopping;
            const icon = els.btnPause.querySelector('[data-feather], svg');
            if (icon) {
                if (icon.tagName === 'svg') {
                    const i = document.createElement('i');
                    i.setAttribute('data-feather', status.paused ? 'play' : 'pause');
                    icon.replaceWith(i);
                    if (typeof feather !== 'undefined') feather.replace();
                } else {
                    icon.setAttribute('data-feather', status.paused ? 'play' : 'pause');
                    if (typeof feather !== 'undefined') feather.replace();
                }
            }
            els.btnPause.title = status.paused ? 'Resume the server' : 'Pause the server';
        }
        if (els.btnSave) els.btnSave.disabled = !status.running;
        if (els.btnQuickSave) els.btnQuickSave.disabled = !status.running;
        if (els.btnUpdate) els.btnUpdate.disabled = status.running;
        if (els.btnReinstall) els.btnReinstall.disabled = status.running;
        if (els.btnUploadWorld) els.btnUploadWorld.disabled = status.running;
        if (els.chatInput) els.chatInput.disabled = !status.running;
        if (els.chatSend) els.chatSend.disabled = !status.running;
        if (typeof ctx.syncCleanupAvailability === 'function') ctx.syncCleanupAvailability();
        if (els.consoleInput) els.consoleInput.disabled = !status.running;
        if (els.consoleSubmit) els.consoleSubmit.disabled = !status.running;
        if (typeof status.storming !== 'undefined' && typeof ctx.updateStormDisplay === 'function') {
            ctx.updateStormDisplay(status.storming);
        }

        const configSubmitButton = els.serverConfigForm ? els.serverConfigForm.querySelector('button[type="submit"]') : null;
        if (configSubmitButton) {
            configSubmitButton.disabled = status.running;
        }

        if (wasRunning && !isRunning) {
            if (typeof ctx.fetchSaves === 'function') {
                ctx.fetchSaves(ctx.getCurrentSavesFilter());
            }
            if (typeof ctx.refreshWorldDownloadOptions === 'function') {
                ctx.refreshWorldDownloadOptions();
            }
        }

        if (isRunning) {
            if (typeof ctx.startUptimeTicker === 'function') {
                ctx.startUptimeTicker();
            }
        } else if (typeof ctx.stopUptimeTicker === 'function') {
            ctx.stopUptimeTicker();
        }
    }

    window.SDSMServerStatusView = {
        applyStatus,
    };
})(window);
