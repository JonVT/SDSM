(function() {
    if (!window.SDSM) {
        window.SDSM = {};
    }

    const serverOptions = window.SDSM_SERVER_OPTIONS || [];

    function createElement(tag, attrs = {}, text = '') {
        const node = document.createElement(tag);
        Object.entries(attrs).forEach(([key, value]) => {
            if (value === null || typeof value === 'undefined') {
                return;
            }
            if (key === 'className') {
                node.className = value;
                return;
            }
            if (key === 'dataset' && value && typeof value === 'object') {
                Object.entries(value).forEach(([dataKey, dataValue]) => {
                    if (typeof dataValue !== 'undefined') {
                        node.dataset[dataKey] = `${dataValue}`;
                    }
                });
                return;
            }
            if (key === 'checked') {
                node.checked = !!value;
                return;
            }
            if (key === 'disabled') {
                node.disabled = !!value;
                return;
            }
            node.setAttribute(key, value);
        });
        if (text) {
            node.textContent = text;
        }
        return node;
    }

    function addFormGroup(form, labelText, control, helpText) {
        const group = createElement('div', { className: 'form-group' });
        if (labelText && control && control.id) {
            const label = createElement('label', { className: 'form-label', for: control.id }, labelText);
            group.appendChild(label);
        }
        group.appendChild(control);
        if (helpText) {
            group.appendChild(createElement('p', { className: 'help-text' }, helpText));
        }
        form.appendChild(group);
    }

    function submitWithHtmx(form, confirmButton) {
        if (!form || typeof htmx === 'undefined') {
            return;
        }
        if (confirmButton) {
            confirmButton.disabled = true;
        }
        htmx.trigger(form, 'submit');
    }

    function showAddUserModal() {
        if (!SDSM.modal || typeof SDSM.modal.info !== 'function') {
            return;
        }

        const form = createElement('form', {
            id: 'addUserForm',
            'hx-post': '/api/users',
            'hx-target': '#user-list',
            'hx-swap': 'beforeend',
            'hx-indicator': '#global-htmx-indicator'
        });

        const usernameInput = createElement('input', {
            type: 'text',
            className: 'form-control',
            id: 'add-username',
            name: 'username',
            minlength: '3',
            autocomplete: 'off',
            required: 'required'
        });
        addFormGroup(form, 'Username', usernameInput);

        const passwordInput = createElement('input', {
            type: 'password',
            className: 'form-control',
            id: 'add-password',
            name: 'password',
            minlength: '8',
            autocomplete: 'new-password',
            required: 'required'
        });
        addFormGroup(form, 'Password', passwordInput, 'Minimum 8 characters.');

        const roleSelect = createElement('select', {
            className: 'form-select',
            id: 'add-role',
            name: 'role'
        });
        roleSelect.appendChild(createElement('option', { value: 'operator' }, 'Operator'));
        roleSelect.appendChild(createElement('option', { value: 'admin' }, 'Admin'));
        roleSelect.value = 'operator';
        addFormGroup(form, 'Role', roleSelect);

        SDSM.modal.info({
            title: 'Add User',
            body: form,
            buttonText: 'Create User',
            onRender: ({ close, confirmButton }) => {
                if (confirmButton) {
                    confirmButton.classList.remove('btn-primary');
                    confirmButton.classList.add('btn-primary');
                    confirmButton.addEventListener('click', (event) => {
                        event.preventDefault();
                        submitWithHtmx(form, confirmButton);
                    });
                }

                form.addEventListener('htmx:afterRequest', (event) => {
                    if (!event.detail?.successful) {
                        if (confirmButton) {
                            confirmButton.disabled = false;
                        }
                        return;
                    }
                    const emptyRow = document.getElementById('user-empty-row');
                    if (emptyRow) {
                        emptyRow.remove();
                    }
                    close();
                    if (typeof htmx !== 'undefined') {
                        htmx.process(document.body);
                    }
                });

                if (typeof htmx !== 'undefined') {
                    htmx.process(form);
                }
            }
        });
    }

    function showResetPasswordModal(username) {
        if (!username || !SDSM.modal || typeof SDSM.modal.info !== 'function') {
            return;
        }

        const form = createElement('form', {
            id: 'resetPasswordForm',
            'hx-post': `/api/users/${encodeURIComponent(username)}/reset-password`,
            'hx-indicator': '#global-htmx-indicator'
        });

        const passwordInput = createElement('input', {
            type: 'password',
            className: 'form-control',
            id: 'reset-password',
            name: 'password',
            minlength: '8',
            autocomplete: 'new-password',
            required: 'required'
        });
        addFormGroup(form, 'New Password', passwordInput, 'Minimum 8 characters.');

        SDSM.modal.info({
            title: `Reset Password · ${username}`,
            body: form,
            buttonText: 'Reset Password',
            onRender: ({ close, confirmButton }) => {
                if (confirmButton) {
                    confirmButton.addEventListener('click', (event) => {
                        event.preventDefault();
                        submitWithHtmx(form, confirmButton);
                    });
                }

                form.addEventListener('htmx:afterRequest', (event) => {
                    if (!event.detail?.successful) {
                        if (confirmButton) {
                            confirmButton.disabled = false;
                        }
                        return;
                    }
                    close();
                });

                if (typeof htmx !== 'undefined') {
                    htmx.process(form);
                }
            }
        });
    }

    function showAccessModal(trigger) {
        if (!trigger || !SDSM.modal || typeof SDSM.modal.info !== 'function') {
            return;
        }

        const username = (trigger.getAttribute('data-username') || '').trim();
        if (!username) {
            return;
        }

        const isAssignedAll = trigger.getAttribute('data-assigned-all') === 'true';
        const assignedServers = (trigger.getAttribute('data-assigned') || '').split(',').filter(Boolean);
        const assignedSet = new Set(assignedServers);

        const form = createElement('form', {
            id: 'accessForm',
            'hx-post': `/api/users/${encodeURIComponent(username)}/assignments`,
            'hx-target': `#user-row-${username}`,
            'hx-swap': 'outerHTML',
            'hx-indicator': '#global-htmx-indicator'
        });

        const assignAllInput = createElement('input', {
            type: 'hidden',
            name: 'assign_all',
            id: 'assign-all-input',
            value: isAssignedAll ? 'true' : 'false'
        });
        form.appendChild(assignAllInput);

        const assignAllTile = createElement('label', { className: 'form-group flex-row items-center gap-3 p-3 rounded-md bg-body-tertiary border' });
        const assignAllToggle = createElement('input', {
            type: 'checkbox',
            id: 'assign-all-toggle',
            className: 'form-switch',
            checked: isAssignedAll
        });
        assignAllTile.appendChild(assignAllToggle);
        const assignAllTextWrap = createElement('div');
        assignAllTextWrap.appendChild(createElement('div', { className: 'font-medium' }, 'Full Access'));
        assignAllTextWrap.appendChild(createElement('p', { className: 'help-text mb-0' }, 'Allow this operator to manage all current and future servers.'));
        assignAllTile.appendChild(assignAllTextWrap);
        form.appendChild(assignAllTile);

        const grid = createElement('div', { className: 'grid grid-cols-2 md:grid-cols-3 gap-2 mt-4', id: 'server-checkbox-grid' });
        if (serverOptions.length > 0) {
            serverOptions.forEach((server) => {
                const tile = createElement('label', { className: 'form-group-tile' });
                const checkbox = createElement('input', {
                    type: 'checkbox',
                    name: 'servers',
                    value: `${server.ID}`,
                    checked: assignedSet.has(String(server.ID))
                });
                tile.appendChild(checkbox);
                tile.appendChild(createElement('span', {}, server.Name || `Server ${server.ID}`));
                grid.appendChild(tile);
            });
        } else {
            grid.appendChild(createElement('p', { className: 'text-muted col-span-full' }, 'No servers available to assign.'));
        }
        form.appendChild(grid);

        SDSM.modal.info({
            title: `Manage Access · ${username}`,
            body: form,
            buttonText: 'Save Access',
            onRender: ({ close, confirmButton }) => {
                const serverCheckboxes = Array.from(form.querySelectorAll('#server-checkbox-grid input[name="servers"]'));

                const setAssignAllState = (isAll) => {
                    assignAllInput.value = isAll ? 'true' : 'false';
                    serverCheckboxes.forEach((checkbox) => {
                        checkbox.disabled = isAll;
                        if (isAll) {
                            checkbox.checked = false;
                        }
                    });
                };

                setAssignAllState(assignAllToggle.checked);
                assignAllToggle.addEventListener('change', () => setAssignAllState(assignAllToggle.checked));

                if (confirmButton) {
                    confirmButton.addEventListener('click', (event) => {
                        event.preventDefault();
                        submitWithHtmx(form, confirmButton);
                    });
                }

                form.addEventListener('htmx:afterRequest', (event) => {
                    if (!event.detail?.successful) {
                        if (confirmButton) {
                            confirmButton.disabled = false;
                        }
                        return;
                    }
                    close();
                    if (typeof htmx !== 'undefined') {
                        htmx.process(document.body);
                    }
                });

                if (typeof htmx !== 'undefined') {
                    htmx.process(form);
                }
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
        const proxy = document.createElement('div');
        proxy.setAttribute('data-username', username);
        proxy.setAttribute('data-assigned-all', trigger.getAttribute('data-assigned-all') || 'false');
        proxy.setAttribute('data-assigned', trigger.getAttribute('data-assigned') || '');
        showAccessModal(proxy);
    }

    if (!document.body.dataset.usersActionBound) {
        document.body.dataset.usersActionBound = 'true';
        document.body.addEventListener('click', (event) => {
            const trigger = event.target.closest('[data-users-action]');
            if (!trigger) {
                return;
            }
            const action = trigger.getAttribute('data-users-action');
            if (!action) {
                return;
            }
            event.preventDefault();
            switch (action) {
                case 'add-user':
                    showAddUserModal();
                    break;
                case 'reset-password':
                    showResetPasswordModal(trigger.getAttribute('data-username') || '');
                    break;
                case 'access':
                    showAccessModal(trigger);
                    break;
                case 'assignment-access':
                    showAssignmentAccessModal(trigger);
                    break;
                default:
                    break;
            }
        });
    }

    window.SDSM.users = {
        showAddUserModal,
        showResetPasswordModal,
        showAccessModal,
        showAssignmentAccessModal
    };
})();
