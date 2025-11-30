(function() {
    if (!window.SDSM) {
        window.SDSM = {};
    }

    function showModal(event) {
        if (event) {
            event.preventDefault();
        }

        const modalContent = `
            <form id="bugReportForm">
                <div class="form-group">
                    <label class="form-label" for="bug_title">Title</label>
                    <input id="bug_title" type="text" class="form-control" maxlength="120" placeholder="Short summary" required />
                </div>
                <div class="form-group">
                    <label class="form-label" for="bug_desc">Description</label>
                    <textarea id="bug_desc" class="form-control" rows="6" placeholder="Steps to reproduce, expected vs actual"></textarea>
                </div>
                <div class="form-group">
                    <label class="form-group-tile">
                        <input id="bug_inc_mgr" type="checkbox" class="form-switch" checked />
                        <span>Include manager log tail</span>
                    </label>
                     <label class="form-group-tile">
                        <input id="bug_inc_upd" type="checkbox" class="form-switch" checked />
                        <span>Include update log tail</span>
                    </label>
                     <label class="form-group-tile">
                        <input id="bug_inc_env" type="checkbox" class="form-switch" checked />
                        <span>Include environment info</span>
                    </label>
                </div>
            </form>
        `;

        function submit() {
            const title = document.getElementById('bug_title').value.trim();
            if (!title) {
                document.getElementById('bug_title').focus();
                return;
            }
            const desc = document.getElementById('bug_desc').value.trim();
            const incMgr = document.getElementById('bug_inc_mgr').checked;
            const incUpd = document.getElementById('bug_inc_upd').checked;
            const incEnv = document.getElementById('bug_inc_env').checked;

            fetch('/api/bug-report', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
                credentials: 'same-origin',
                body: JSON.stringify({
                    title: title,
                    description: desc,
                    include_manager_log: incMgr,
                    include_update_log: incUpd,
                    include_environment: incEnv
                })
            })
            .then(response => {
                if (!response.ok) {
                    return response.json().then(err => { throw err; });
                }
                return response.json();
            })
            .then(() => {
                SDSM.modal.hide();
                htmx.trigger(document.body, 'showToast', {
                    type: 'success',
                    title: 'Thanks!',
                    message: 'Bug report submitted successfully.'
                });
            })
            .catch(err => {
                console.error('Bug report submission failed:', err);
                htmx.trigger(document.body, 'showToast', {
                    type: 'error',
                    title: 'Submit Failed',
                    message: (err && err.error) || 'Unable to submit bug report.'
                });
            });
        }

        SDSM.modal.show({
            title: 'Report a Bug',
            content: modalContent,
            buttons: [
                { label: 'Cancel', class: 'btn-secondary', action: 'hide' },
                { label: 'Submit', class: 'btn-primary', action: submit }
            ]
        });
    }

    window.SDSM.bugReport = {
        showModal
    };

})();
