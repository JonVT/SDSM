(function() {
    if (window.SDSM && window.SDSM.modal) return;

    function prefersReducedMotion() {
        try {
            return !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
        } catch (_) {
            return false;
        }
    }

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
        if (bodyEl && typeof opts.body !== 'undefined' && opts.body !== null) {
            bodyEl.replaceChildren();
            if (typeof opts.body === 'string') {
                bodyEl.textContent = opts.body;
            } else if (opts.body instanceof Node) {
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
                const delay = prefersReducedMotion() ? 0 : 100;
                setTimeout(() => {
                    try {
                        auto.focus();
                        if (typeof auto.select === 'function') auto.select();
                    } catch (_) {}
                }, delay);
            }
            resolve();
        });
    }

    function cleanup(modal, restoreFocus, trapDisposer) {
        modal.classList.remove('active');
        modal.setAttribute('aria-hidden', 'true');
        if (trapDisposer) trapDisposer();

        const removeDelay = prefersReducedMotion() ? 0 : 300;
        setTimeout(() => {
            if (modal && modal.parentNode) {
                modal.parentNode.removeChild(modal);
            }
            if (restoreFocus) {
                try {
                    restoreFocus.focus();
                } catch (_) {}
            }
        }, removeDelay);
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
            label: 'Enter a value',
            placeholder: '',
            defaultValue: '',
            confirmText: 'OK',
            cancelText: 'Cancel',
            hint: '',
            danger: false,
            validate: null,
            ...options
        };
        if (opts.message && typeof opts.body === 'undefined') opts.body = opts.message;

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
            input.setAttribute('aria-invalid', 'false');
        }
        if (hintEl) {
            if (opts.hint) {
                hintEl.textContent = opts.hint;
                hintEl.classList.remove('hidden');
                hintEl.style.color = '';
            } else {
                hintEl.classList.add('hidden');
            }
        }

        const confirmBtn = setButton(modal, '[data-confirm]', opts.confirmText);
        const cancelBtn = setButton(modal, '[data-cancel]', opts.cancelText);
        if (opts.danger && confirmBtn) {
            confirmBtn.classList.remove('btn-primary');
            confirmBtn.classList.add('btn-danger');
        }

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
                                hintEl.style.color = 'var(--danger-500)';
                            }
                            if (input) input.setAttribute('aria-invalid', 'true');
                            if (input) input.focus();
                            return;
                        }
                    } catch (err) {
                        if (hintEl) {
                            hintEl.textContent = (err && err.message) || 'Invalid';
                            hintEl.classList.remove('hidden');
                            hintEl.style.color = 'var(--danger-500)';
                        }
                        if (input) input.setAttribute('aria-invalid', 'true');
                        return;
                    }
                }
                if (input) input.setAttribute('aria-invalid', 'false');
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

            if (typeof opts.onRender === 'function') {
                try {
                    opts.onRender({
                        modal,
                        close: finish,
                        confirmButton: okBtn
                    });
                } catch (err) {
                    console.error('modal info onRender failed:', err);
                }
            }
            
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
