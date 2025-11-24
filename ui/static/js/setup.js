function runSetup() {
    const setupBtn = document.getElementById('setupBtn');
    const skipBtn = document.getElementById('skipBtn');
    const updateBtn = document.getElementById('updateBtn');
    const progress = document.getElementById('progress');
    const progressFill = document.getElementById('progressFill');
    const progressText = document.getElementById('progressText');
    const progressDetails = document.getElementById('progressDetails');
    const installSection = document.getElementById('install-section');
    const errorBox = document.getElementById('errorBox');

    setupBtn.classList.add('hidden');
    skipBtn.classList.add('hidden');
    updateBtn.classList.add('hidden');
    progress.classList.remove('hidden');
    installSection.classList.add('hidden');
    errorBox.classList.add('hidden');

    progressText.textContent = '⏳ Starting setup...';
    progressFill.style.width = '0%';
    progressDetails.innerHTML = '';

    const eventSource = new EventSource('/setup/run');

    eventSource.onmessage = function(event) {
        const data = JSON.parse(event.data);

        if (data.error) {
            progressText.textContent = '❌ Error during setup.';
            progressDetails.innerHTML = `<p class="text-danger">${data.error}</p>`;
            eventSource.close();
            setupBtn.classList.remove('hidden');
            skipBtn.classList.remove('hidden');
            return;
        }

        if (data.message) {
            progressText.textContent = `⏳ ${data.message}`;
        }

        if (data.percentage) {
            progressFill.style.width = data.percentage + '%';
        }

        if (data.details) {
            const p = document.createElement('p');
            p.textContent = data.details;
            progressDetails.appendChild(p);
            progressDetails.scrollTop = progressDetails.scrollHeight;
        }

        if (data.complete) {
            progressText.textContent = '✅ Setup Complete!';
            progressFill.style.width = '100%';
            eventSource.close();
            setTimeout(() => {
                window.location.href = '/';
            }, 2000);
        }
    };

    eventSource.onerror = function(err) {
        progressText.textContent = '❌ Connection error.';
        progressDetails.innerHTML = '<p class="text-danger">Lost connection to the server. Please check the logs and try again.</p>';
        eventSource.close();
        setupBtn.classList.remove('hidden');
        skipBtn.classList.remove('hidden');
    };
}

function runUpdate() {
    const setupBtn = document.getElementById('setupBtn');
    const skipBtn = document.getElementById('skipBtn');
    const updateBtn = document.getElementById('updateBtn');
    const progress = document.getElementById('progress');
    const progressFill = document.getElementById('progressFill');
    const progressText = document.getElementById('progressText');
    const progressDetails = document.getElementById('progressDetails');
    const installSection = document.getElementById('install-section');
    const errorBox = document.getElementById('errorBox');

    setupBtn.classList.add('hidden');
    skipBtn.classList.add('hidden');
    updateBtn.classList.add('hidden');
    progress.classList.remove('hidden');
    installSection.classList.add('hidden');
    errorBox.classList.add('hidden');

    progressText.textContent = '⏳ Starting update...';
    progressFill.style.width = '0%';
    progressDetails.innerHTML = '';

    const eventSource = new EventSource('/setup/update');

    eventSource.onmessage = function(event) {
        const data = JSON.parse(event.data);

        if (data.error) {
            progressText.textContent = '❌ Error during update.';
            progressDetails.innerHTML = `<p class="text-danger">${data.error}</p>`;
            eventSource.close();
            updateBtn.classList.remove('hidden');
            return;
        }

        if (data.message) {
            progressText.textContent = `⏳ ${data.message}`;
        }

        if (data.percentage) {
            progressFill.style.width = data.percentage + '%';
        }

        if (data.details) {
            const p = document.createElement('p');
            p.textContent = data.details;
            progressDetails.appendChild(p);
            progressDetails.scrollTop = progressDetails.scrollHeight;
        }

        if (data.complete) {
            progressText.textContent = '✅ Update Complete!';
            progressFill.style.width = '100%';
            eventSource.close();
            setTimeout(() => {
                window.location.href = '/';
            }, 2000);
        }
    };

    eventSource.onerror = function(err) {
        progressText.textContent = '❌ Connection error.';
        progressDetails.innerHTML = '<p class="text-danger">Lost connection to the server. Please check the logs and try again.</p>';
        eventSource.close();
        updateBtn.classList.remove('hidden');
    };
}

document.addEventListener('DOMContentLoaded', function() {
    const updatesHint = document.getElementById('updatesHint');
    const setupBtn = document.getElementById('setupBtn');
    const updateBtn = document.getElementById('updateBtn');
    const installList = document.getElementById('install-list');

    // If updates are available and there are no required items to install,
    // show the update button and hide the setup button.
    if (!updatesHint.classList.contains('hidden') && installList.children.length === 0) {
        setupBtn.classList.add('hidden');
        updateBtn.classList.remove('hidden');
    }
});
