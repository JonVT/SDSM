function runSetup() {
    startWorkflow({
        action: '/setup/install',
        label: 'setup',
        completeText: '✅ Setup Complete!',
        retryButtonId: 'setupBtn',
        redirectDelayMs: 2000,
    });
}

function runUpdate() {
    startWorkflow({
        action: '/setup/update',
        label: 'update',
        completeText: '✅ Update Complete!',
        retryButtonId: 'updateBtn',
        redirectDelayMs: 2000,
    });
}

function startWorkflow({ action, label, completeText, retryButtonId, redirectDelayMs }) {
    const setupBtn = document.getElementById('setupBtn');
    const skipBtn = document.getElementById('skipBtn');
    const updateBtn = document.getElementById('updateBtn');
    const progress = document.getElementById('progress');
    const progressFill = document.getElementById('progressFill');
    const progressText = document.getElementById('progressText');
    const progressDetails = document.getElementById('progressDetails');
    const installSection = document.getElementById('install-section');
    const errorBox = document.getElementById('errorBox');

    const activeRetryButton = retryButtonId === 'setupBtn' ? setupBtn : updateBtn;

    if (!setupBtn || !skipBtn || !updateBtn || !progress || !progressFill || !progressText || !progressDetails || !installSection || !errorBox) {
        return;
    }

    setupBtn.classList.add('hidden');
    skipBtn.classList.add('hidden');
    updateBtn.classList.add('hidden');
    progress.classList.remove('hidden');
    installSection.classList.add('hidden');
    errorBox.classList.add('hidden');

    progressText.textContent = `⏳ Starting ${label}...`;
    progressFill.style.width = '0%';
    progressDetails.textContent = '';

    let pollTimer = null;
    let canceled = false;

    const clearPolling = () => {
        canceled = true;
        if (pollTimer) {
            clearTimeout(pollTimer);
            pollTimer = null;
        }
    };

    const showRetry = () => {
        if (activeRetryButton) {
            activeRetryButton.classList.remove('hidden');
        }
    };

    const showFailure = (title, message) => {
        progressText.textContent = title;
        progressDetails.textContent = '';
        const errorP = document.createElement('p');
        errorP.className = 'text-danger';
        errorP.textContent = message;
        progressDetails.appendChild(errorP);
        clearPolling();
        showRetry();
    };

    const renderSnapshot = (snapshot) => {
        const stages = Array.isArray(snapshot?.stages) ? snapshot.stages : [];
        const overall = Number(snapshot?.overall_percent);
        progressFill.style.width = `${Number.isFinite(overall) ? Math.max(0, Math.min(100, overall)) : 0}%`;

        const activeStage = stages.find((stage) => stage && String(stage.status || '').toLowerCase() === 'running');
        if (activeStage) {
            const stageName = activeStage.display_name || activeStage.component || 'Processing';
            const stagePercent = Number(activeStage.percent);
            const percentLabel = Number.isFinite(stagePercent) ? ` (${Math.max(0, Math.min(100, stagePercent))}%)` : '';
            progressText.textContent = `⏳ ${stageName}${percentLabel}`;
        } else if (snapshot?.in_progress) {
            progressText.textContent = `⏳ ${label === 'setup' ? 'Preparing setup...' : 'Preparing update...'}`;
        }

        progressDetails.textContent = '';
        const detailList = document.createElement('div');
        detailList.className = 'progress-details-list';

        stages.forEach((stage) => {
            if (!stage) {
                return;
            }
            const row = document.createElement('p');
            const displayName = stage.display_name || stage.component || 'Component';
            const status = String(stage.status || 'Pending');
            const percent = Number(stage.percent);
            const suffix = Number.isFinite(percent) ? ` — ${Math.max(0, Math.min(100, percent))}%` : '';
            const duration = Number(stage.duration_ms) > 0 ? ` (${Math.round(Number(stage.duration_ms) / 1000)}s)` : '';
            row.textContent = `${displayName}: ${status}${suffix}${duration}`;
            if (status.toLowerCase() === 'error') {
                row.className = 'text-danger';
            }
            detailList.appendChild(row);
        });

        if (stages.length === 0) {
            const row = document.createElement('p');
            row.textContent = 'Waiting for progress updates...';
            detailList.appendChild(row);
        }

        progressDetails.appendChild(detailList);
        progressDetails.scrollTop = progressDetails.scrollHeight;
    };

    const finish = (text) => {
        progressText.textContent = text;
        progressFill.style.width = '100%';
        clearPolling();
        setTimeout(() => {
            window.location.href = '/';
        }, redirectDelayMs);
    };

    const pollProgress = async () => {
        if (canceled) {
            return;
        }
        try {
            const resp = await fetch('/setup/progress', {
                method: 'GET',
                headers: {
                    Accept: 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                },
                credentials: 'same-origin',
            });
            if (!resp.ok) {
                throw new Error(`Progress request failed (${resp.status})`);
            }
            const snapshot = await resp.json();
            renderSnapshot(snapshot);

            const stages = Array.isArray(snapshot?.stages) ? snapshot.stages : [];
            const hasError = stages.some((stage) => String(stage?.status || '').toLowerCase() === 'error');
            const isRunning = !!snapshot?.in_progress;
            const overall = Number(snapshot?.overall_percent);
            const completedEnough = Number.isFinite(overall) && overall >= 100;

            if (hasError && !isRunning) {
                const failedStage = stages.find((stage) => String(stage?.status || '').toLowerCase() === 'error');
                showFailure(`❌ ${label === 'setup' ? 'Setup' : 'Update'} failed.`, failedStage?.last_line ? String(failedStage.last_line) : 'One or more components failed to update.');
                return;
            }

            if (!isRunning && completedEnough) {
                finish(completeText);
                return;
            }

            if (!isRunning && !completedEnough) {
                showFailure(`❌ ${label === 'setup' ? 'Setup' : 'Update'} did not complete.`, 'The progress feed ended before completion. Please check the logs and try again.');
                return;
            }

            pollTimer = setTimeout(pollProgress, 2000);
        } catch (error) {
            showFailure('❌ Connection error.', 'Lost connection to the server. Please check the logs and try again.');
        }
    };

    const startRequest = async () => {
        try {
            const resp = await fetch(action, {
                method: 'POST',
                headers: {
                    Accept: 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                },
                credentials: 'same-origin',
            });

            let data = {};
            try {
                data = await resp.json();
            } catch (_) {
                data = {};
            }

            if (!resp.ok) {
                throw new Error(data.message || data.error || `Unable to start ${label}.`);
            }

            progressText.textContent = data.message ? `⏳ ${data.message}` : `⏳ ${label === 'setup' ? 'Setup started' : 'Update started'}`;
            pollProgress();
        } catch (error) {
            showFailure(`❌ Error starting ${label}.`, String(error.message || error));
        }
    };

    startRequest();
}

document.addEventListener('DOMContentLoaded', function() {
    const updatesHint = document.getElementById('updatesHint');
    const setupBtn = document.getElementById('setupBtn');
    const updateBtn = document.getElementById('updateBtn');
    const installList = document.getElementById('install-list');

    document.body.addEventListener('click', function(event) {
        const trigger = event.target.closest('[data-setup-action]');
        if (!trigger) {
            return;
        }
        event.preventDefault();
        const action = trigger.getAttribute('data-setup-action');
        if (action === 'start') {
            runSetup();
        } else if (action === 'update') {
            runUpdate();
        }
    });

    // If updates are available and there are no required items to install,
    // show the update button and hide the setup button.
    if (updatesHint && installList && setupBtn && updateBtn && !updatesHint.classList.contains('hidden') && installList.children.length === 0) {
        setupBtn.classList.add('hidden');
        updateBtn.classList.remove('hidden');
    }
});
