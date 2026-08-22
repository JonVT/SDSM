(function() {
    if (!window.SDSM) {
        window.SDSM = {};
    }

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
            if (key === 'checked') {
                node.checked = !!value;
                return;
            }
            node.setAttribute(key, value);
        });
        if (text) {
            node.textContent = text;
        }
        return node;
    }

    function buildForm() {
        const form = createElement('form', { id: 'bugReportForm' });

        const titleGroup = createElement('div', { className: 'form-group' });
        titleGroup.appendChild(createElement('label', { className: 'form-label', for: 'bug_title' }, 'Title'));
        titleGroup.appendChild(createElement('input', {
            id: 'bug_title',
            type: 'text',
            className: 'form-control',
            maxlength: '120',
            placeholder: 'Short summary',
            required: 'required'
        }));
        form.appendChild(titleGroup);

        const descGroup = createElement('div', { className: 'form-group' });
        descGroup.appendChild(createElement('label', { className: 'form-label', for: 'bug_desc' }, 'Description'));
        descGroup.appendChild(createElement('textarea', {
            id: 'bug_desc',
            className: 'form-control',
            rows: '6',
            placeholder: 'Steps to reproduce, expected vs actual'
        }));
        form.appendChild(descGroup);

        const optionsGroup = createElement('div', { className: 'form-group' });
        const options = [
            { id: 'bug_inc_mgr', label: 'Include manager log tail', checked: true },
            { id: 'bug_inc_upd', label: 'Include update log tail', checked: true },
            { id: 'bug_inc_env', label: 'Include environment info', checked: true }
        ];

        options.forEach((option) => {
            const tile = createElement('label', { className: 'form-group-tile' });
            tile.appendChild(createElement('input', {
                id: option.id,
                type: 'checkbox',
                className: 'form-switch',
                checked: option.checked
            }));
            tile.appendChild(createElement('span', {}, option.label));
            optionsGroup.appendChild(tile);
        });

        form.appendChild(optionsGroup);
        return form;
    }

    function showModal(event) {
        if (event) {
            event.preventDefault();
        }
        if (!SDSM.modal || typeof SDSM.modal.info !== 'function') {
            return;
        }

        const form = buildForm();

        SDSM.modal.info({
            title: 'Report a Bug',
            body: form,
            buttonText: 'Submit',
            onRender: ({ close, confirmButton }) => {
                const submit = () => {
                    const titleInput = document.getElementById('bug_title');
                    if (!titleInput) {
                        return;
                    }
                    const title = titleInput.value.trim();
                    if (!title) {
                        titleInput.focus();
                        return;
                    }

                    if (confirmButton) {
                        confirmButton.disabled = true;
                    }

                    const desc = (document.getElementById('bug_desc')?.value || '').trim();
                    const incMgr = !!document.getElementById('bug_inc_mgr')?.checked;
                    const incUpd = !!document.getElementById('bug_inc_upd')?.checked;
                    const incEnv = !!document.getElementById('bug_inc_env')?.checked;

                    fetch('/api/bug-report', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
                        credentials: 'same-origin',
                        body: JSON.stringify({
                            title,
                            description: desc,
                            include_manager_log: incMgr,
                            include_update_log: incUpd,
                            include_environment: incEnv
                        })
                    })
                        .then(async (response) => {
                            if (response.ok) {
                                return response.json().catch(() => ({}));
                            }
                            let payload = {};
                            try {
                                payload = await response.json();
                            } catch (_) {
                                // ignore parse errors
                            }
                            throw payload;
                        })
                        .then(() => {
                            close();
                            if (typeof htmx !== 'undefined') {
                                htmx.trigger(document.body, 'showToast', {
                                    type: 'success',
                                    title: 'Thanks!',
                                    message: 'Bug report submitted successfully.'
                                });
                            }
                        })
                        .catch((err) => {
                            console.error('Bug report submission failed:', err);
                            if (typeof htmx !== 'undefined') {
                                htmx.trigger(document.body, 'showToast', {
                                    type: 'error',
                                    title: 'Submit Failed',
                                    message: (err && err.error) || 'Unable to submit bug report.'
                                });
                            }
                        })
                        .finally(() => {
                            if (confirmButton) {
                                confirmButton.disabled = false;
                            }
                        });
                };

                if (confirmButton) {
                    confirmButton.addEventListener('click', (ev) => {
                        ev.preventDefault();
                        submit();
                    });
                }

                form.addEventListener('submit', (ev) => {
                    ev.preventDefault();
                    submit();
                });
            }
        });
    }

    window.SDSM.bugReport = {
        showModal
    };
})();
