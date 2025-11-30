(function() {
    if (!window.SDSM) {
        window.SDSM = {};
    }

    const serverOptions = window.SDSM_SERVER_OPTIONS || [];

    function showAddUserModal() {
        const modalContent = `
            <form id="addUserForm" hx-post="/api/users" hx-target="#user-list" hx-swap="beforeend" hx-indicator="#global-htmx-indicator"
                  hx-on:htmx:after-request="if(event.detail.successful) {
                      const emptyRow = document.getElementById('user-empty-row');
                      if (emptyRow) { emptyRow.remove(); }
                      SDSM.modal.hide();
                      htmx.process(document.body); // Re-process for new feather icons
                  }">
                <div class="form-group">
                    <label for="add-username" class="form-label">Username</label>
                    <input type="text" class="form-control" id="add-username" name="username" minlength="3" autocomplete="off" required>
                </div>
                <div class="form-group">
                    <label for="add-password" class="form-label">Password</label>
                    <input type="password" class="form-control" id="add-password" name="password" minlength="8" autocomplete="new-password" required>
                    <p class="help-text">Minimum 8 characters.</p>
                </div>
                <div class="form-group">
                    <label for="add-role" class="form-label">Role</label>
                    <select class="form-select" id="add-role" name="role">
                        <option value="operator" selected>Operator</option>
                        <option value="admin">Admin</option>
                    </select>
                </div>
            </form>
        `;

        SDSM.modal.show({
            title: 'Add User',
            subtitle: 'Create a new login with operator or admin access.',
            content: modalContent,
            buttons: [
                { label: 'Cancel', class: 'btn-secondary', action: 'hide' },
                { label: 'Create User', class: 'btn-primary', action: () => htmx.submit(document.getElementById('addUserForm')) }
            ],
            onShow: () => {
                htmx.process(document.getElementById('addUserForm'));
            }
        });
    }

    function showResetPasswordModal(username) {
        const modalContent = `
            <form id="resetPasswordForm" hx-post="/api/users/${encodeURIComponent(username)}/reset-password" hx-indicator="#global-htmx-indicator"
                  hx-on:htmx:after-request="if(event.detail.successful) { SDSM.modal.hide(); }">
                <div class="form-group">
                    <label for="reset-password" class="form-label">New Password</label>
                    <input type="password" class="form-control" id="reset-password" name="password" minlength="8" autocomplete="new-password" required>
                    <p class="help-text">Minimum 8 characters.</p>
                </div>
            </form>
        `;

        SDSM.modal.show({
            title: 'Reset Password',
            subtitle: `Update the password for ${username}.`,
            content: modalContent,
            buttons: [
                { label: 'Cancel', class: 'btn-secondary', action: 'hide' },
                { label: 'Reset Password', class: 'btn-primary', action: () => htmx.submit(document.getElementById('resetPasswordForm')) }
            ],
            onShow: () => {
                htmx.process(document.getElementById('resetPasswordForm'));
            }
        });
    }

    function showAccessModal(trigger) {
        const username = (trigger.getAttribute('data-username') || '').trim();
        if (!username) return;

        const isAssignedAll = trigger.getAttribute('data-assigned-all') === 'true';
        const assignedServers = (trigger.getAttribute('data-assigned') || '').split(',').filter(Boolean);
        const assignedSet = new Set(assignedServers);

        let serverCheckboxesHTML = '';
        if (serverOptions.length > 0) {
            serverCheckboxesHTML = serverOptions.map(server => `
                <label class="form-group-tile">
                    <input type="checkbox" name="servers" value="${server.ID}" ${assignedSet.has(String(server.ID)) ? 'checked' : ''}>
                    <span>${server.Name}</span>
                </label>
            `).join('');
        } else {
            serverCheckboxesHTML = '<p class="text-muted col-span-full">No servers available to assign.</p>';
        }

        const modalContent = `
            <form id="accessForm" hx-post="/api/users/${encodeURIComponent(username)}/assignments" hx-target="#user-row-${username}" hx-swap="outerHTML" hx-indicator="#global-htmx-indicator"
                  hx-on:htmx:after-request="if(event.detail.successful) { SDSM.modal.hide(); htmx.process(document.body); }">
                <input type="hidden" name="assign_all" id="assign-all-input" value="${isAssignedAll ? 'true' : 'false'}">
                <label class="form-group flex-row items-center gap-3 p-3 rounded-md bg-body-tertiary border">
                    <input type="checkbox" id="assign-all-toggle" class="form-switch" ${isAssignedAll ? 'checked' : ''}>
                    <div>
                        <div class="font-medium">Full Access</div>
                        <p class="help-text mb-0">Allow this operator to manage all current and future servers.</p>
                    </div>
                </label>
                <div class="grid grid-cols-2 md:grid-cols-3 gap-2 mt-4" id="server-checkbox-grid">
                    ${serverCheckboxesHTML}
                </div>
            </form>
        `;

        SDSM.modal.show({
            title: 'Manage Access',
            subtitle: `Choose which servers ${username} can control.`,
            content: modalContent,
            size: 'lg',
            buttons: [
                { label: 'Cancel', class: 'btn-secondary', action: 'hide' },
                { label: 'Save Access', class: 'btn-primary', action: () => htmx.submit(document.getElementById('accessForm')) }
            ],
            onShow: () => {
                htmx.process(document.getElementById('accessForm'));
                const assignAllToggle = document.getElementById('assign-all-toggle');
                const assignAllInput = document.getElementById('assign-all-input');
                const serverCheckboxes = document.querySelectorAll('#server-checkbox-grid input[name="servers"]');

                const setAssignAllState = (isAll) => {
                    assignAllInput.value = isAll ? 'true' : 'false';
                    serverCheckboxes.forEach(cb => {
                        cb.disabled = isAll;
                        if (isAll) cb.checked = false;
                    });
                };

                setAssignAllState(assignAllToggle.checked);
                assignAllToggle.addEventListener('change', () => setAssignAllState(assignAllToggle.checked));
            }
        });
    }

    function showAssignmentAccessModal(trigger) {
        if (!trigger) {
            return;
        }
        const username = (trigger.getAttribute('data-username') || '').trim();
        if (!username) {
            return;
        }
        const assignedAll = trigger.getAttribute('data-assigned-all') || 'false';
        const assigned = trigger.getAttribute('data-assigned') || '';
        const proxy = document.createElement('div');
        proxy.setAttribute('data-username', username);
        proxy.setAttribute('data-assigned-all', assignedAll);
        proxy.setAttribute('data-assigned', assigned);
        showAccessModal(proxy);
    }

    window.SDSM.users = {
        showAddUserModal,
        showResetPasswordModal,
        showAccessModal,
        showAssignmentAccessModal
    };

})();