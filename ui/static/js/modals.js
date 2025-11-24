(function() {
    if (window.SDSM && window.SDSM.modal) return;

    function cloneTemplate(id) {
        const tpl = document.getElementById(id);
        return tpl ? tpl.content.cloneNode(true) : null;
    }

    function focusTrap(modal, onDeactivate) {
        const focusable = Array.from(modal.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')).filter(el => !el.disabled);
        if (focusable.length === 0) return () => {};

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        function onKey(e) {
            if (e.key === 'Tab') {
                if (e.shiftKey) {
                    if (document.activeElement === first) {
                        e.preventDefault();
                        last.focus();
                    }
                } else {
                    if (document.activeElement === last) {
                        e.preventDefault();
                        first.focus();
                    }
                }
            } else if (e.key === 'Escape') {
                onDeactivate();
            }
        }
        modal.addEventListener('keydown', onKey);
        return () => modal.removeEventListener('keydown', onKey);
    }

    function buildModal(templateId, opts) {
        const frag = cloneTemplate(templateId);
        if (!frag) return null;

        const modal = frag.querySelector('.modal');
        if (!modal) return null;

        document.body.appendChild(frag);

        const titleEl = modal.querySelector('.modal-title');
        const bodyEl = modal.querySelector('.modal-body');

        if (titleEl && opts.title) titleEl.textContent = opts.title;
        if (bodyEl && opts.body) {
            if (typeof opts.body === 'string') {
                bodyEl.innerHTML = opts.body;
            } else if (opts.body instanceof Node) {
                bodyEl.innerHTML = '';
                bodyEl.appendChild(opts.body);
            }
        }
        return modal;
    }

    function setButton(modal, selector, text, visible = true) {
        const btn = modal.querySelector(selector);
        if (btn) {
            if (text) btn.textContent = text;
            if (!visible) btn.classList.add('hidden');
        }
        return btn;
    }

    function activate(modal) {
        return new Promise(resolve => {
            modal.classList.add('active');
            modal.setAttribute('aria-hidden', 'false');
            const auto = modal.querySelector('input, select, textarea') || modal.querySelector('.btn-primary') || modal.querySelector('.btn');
            if (auto) {
                setTimeout(() => {
                    try {
                        auto.focus();
                        if (typeof auto.select === 'function') auto.select();
                    } catch (_) {}
                }, 100); // Small delay for transition
            }
            resolve();
        });
    }

    function cleanup(modal, restoreFocus, trapDisposer) {
        modal.classList.remove('active');
        modal.setAttribute('aria-hidden', 'true');
        if (trapDisposer) trapDisposer();
        
        // Let animation finish before removing
        setTimeout(() => {
            if (modal && modal.parentNode) {
                modal.parentNode.removeChild(modal);
            }
            if (restoreFocus) {
                try {
                    restoreFocus.focus();
                } catch (_) {}
            }
        }, 300);
    }

    function openConfirm(options) {
        const opts = {
            title: 'Confirm Action',
            body: 'Are you sure?',
            confirmText: 'Confirm',
            cancelText: 'Cancel',
            danger: false,
            ...options
        };
        if (opts.message && !opts.body) opts.body = opts.message;

        const prev = document.activeElement;
        const modal = buildModal('tpl-modal-confirm', opts);
        if (!modal) return Promise.resolve(false);

        const confirmBtn = setButton(modal, '[data-confirm]', opts.confirmText);
        const cancelBtn = setButton(modal, '[data-cancel]', opts.cancelText);
        if (opts.danger && confirmBtn) {
            confirmBtn.classList.remove('btn-primary');
            confirmBtn.classList.add('btn-danger');
        }

        return new Promise(resolve => {
            let trapDisposer;
            const done = (val) => {
                cleanup(modal, prev, trapDisposer);
                resolve(val);
            };
            
            confirmBtn.addEventListener('click', () => done(true));
            cancelBtn.addEventListener('click', () => done(false));
            modal.addEventListener('click', e => {
                if (e.target === modal) done(false);
            });
            
            trapDisposer = focusTrap(modal, () => done(false));
            activate(modal);
        });
    }

    function openPrompt(options) {
        const opts = {
            title: 'Input Required',
            body: '',
            label: 'Enter a value',
            placeholder: '',
            defaultValue: '',
            confirmText: 'OK',
            cancelText: 'Cancel',
            hint: '',
            validate: null,
            ...options
        };
        if (opts.message && !opts.body) opts.body = opts.message;

        const prev = document.activeElement;
        const modal = buildModal('tpl-modal-prompt', opts);
        if (!modal) return Promise.resolve(null);

        const input = modal.querySelector('[data-input]');
        const labelEl = modal.querySelector('[data-label]');
        const hintEl = modal.querySelector('.form-text');

        if (labelEl && opts.label) labelEl.textContent = opts.label;
        if (input) {
            input.placeholder = opts.placeholder || '';
            input.value = opts.defaultValue || '';
        }
        if (hintEl) {
            if (opts.hint) {
                hintEl.textContent = opts.hint;
                hintEl.classList.remove('hidden');
            } else {
                hintEl.classList.add('hidden');
            }
        }

        const confirmBtn = setButton(modal, '[data-confirm]', opts.confirmText);
        const cancelBtn = setButton(modal, '[data-cancel]', opts.cancelText);

        return new Promise(resolve => {
            let trapDisposer;
            const finish = (val) => {
                cleanup(modal, prev, trapDisposer);
                resolve(val);
            };

            const commit = () => {
                const raw = (input ? input.value : '').trim();
                if (opts.validate) {
                    try {
                        const res = opts.validate(raw);
                        if (res !== true) {
                            if (hintEl) {
                                hintEl.textContent = res || 'Invalid value';
                                hintEl.classList.remove('hidden');
                                hintEl.style.color = 'var(--color-danger)';
                            }
                            if (input) input.focus();
                            return;
                        }
                    } catch (err) {
                        if (hintEl) {
                            hintEl.textContent = (err && err.message) || 'Invalid';
                            hintEl.classList.remove('hidden');
                            hintEl.style.color = 'var(--color-danger)';
                        }
                        return;
                    }
                }
                finish(raw);
            };

            confirmBtn.addEventListener('click', commit);
            cancelBtn.addEventListener('click', () => finish(null));
            modal.addEventListener('click', e => {
                if (e.target === modal) finish(null);
            });
            if (input) {
                input.addEventListener('keydown', e => {
                    if (e.key === 'Enter') {
                        e.preventDefault();
                        commit();
                    }
                });
            }
            
            trapDisposer = focusTrap(modal, () => finish(null));
            activate(modal);
        });
    }

    function openInfo(options) {
        const opts = {
            title: 'Information',
            body: '',
            buttonText: 'OK',
            ...options
        };
        if (opts.message && !opts.body) opts.body = opts.message;
        if (opts.confirmText && !opts.buttonText) opts.buttonText = opts.confirmText;

        const prev = document.activeElement;
        const modal = buildModal('tpl-modal-info', opts);
        if (!modal) return Promise.resolve();

        const okBtn = setButton(modal, '[data-confirm]', opts.buttonText);

        return new Promise(resolve => {
            let trapDisposer;
            const finish = () => {
                cleanup(modal, prev, trapDisposer);
                resolve();
            };
            
            okBtn.addEventListener('click', finish);
            modal.addEventListener('click', e => {
                if (e.target === modal) finish();
            });

            trapDisposer = focusTrap(modal, finish);
            activate(modal);
        });
    }

    // Assign to a global namespace
    window.SDSM = window.SDSM || {};
    window.SDSM.modal = {
        confirm: openConfirm,
        prompt: openPrompt,
        info: openInfo,
    };

    // Legacy support
    window.openConfirm = openConfirm;
    window.openPrompt = openPrompt;
    window.openInfo = openInfo;

})();
