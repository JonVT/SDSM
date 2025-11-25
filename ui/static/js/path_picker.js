(function (window, document) {
  'use strict';

  function normalizeMode(mode) {
    const value = String(mode || '').toLowerCase();
    if (value === 'file' || value === 'files' || value === 'path' || value === 'paths') {
      return 'file';
    }
    if (value === 'any' || value === 'all' || value === 'both') {
      return 'any';
    }
    return 'directory';
  }

  function formatTimestamp(value) {
    if (!value) {
      return '';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return '';
    }
    return date.toLocaleString();
  }

  function formatSize(bytes) {
    const numeric = Number(bytes);
    if (!Number.isFinite(numeric) || numeric <= 0) {
      return '';
    }
    if (window.SDSM && window.SDSM.ui && typeof window.SDSM.ui.formatBytes === 'function') {
      return window.SDSM.ui.formatBytes(numeric);
    }
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = numeric;
    let idx = 0;
    while (size >= 1024 && idx < units.length - 1) {
      size /= 1024;
      idx++;
    }
    const precision = idx === 0 ? 0 : 1;
    return `${size.toFixed(precision)} ${units[idx]}`;
  }

  function open(options = {}) {
    const template = document.getElementById('tpl-modal-path-picker');
    if (!template || !template.content) {
      return Promise.resolve(null);
    }
    const fragment = template.content.cloneNode(true);
    const modal = fragment.querySelector('.modal');
    if (!modal) {
      return Promise.resolve(null);
    }

    const settings = {
      title: options.title || 'Select Path',
      description: options.description || '',
      confirmText: options.confirmText || 'Use Path',
      initialPath: options.initialPath || options.value || '',
      mode: normalizeMode(options.mode),
      emptySelectionLabel: options.selectionEmptyText || 'No path selected',
    };

    return new Promise((resolve) => {
      const refs = {
        title: modal.querySelector('[data-title]'),
        description: modal.querySelector('[data-description]'),
        breadcrumbs: modal.querySelector('[data-breadcrumbs]'),
        currentPath: modal.querySelector('[data-current-path]'),
        entries: modal.querySelector('[data-entries]'),
        emptyState: modal.querySelector('[data-empty-state]'),
        error: modal.querySelector('[data-error-region]'),
        selectedPath: modal.querySelector('[data-selected-path]'),
        confirm: modal.querySelector('[data-confirm]'),
        cancel: modal.querySelector('[data-cancel]'),
        close: modal.querySelector('[data-close]'),
        useCurrent: modal.querySelector('[data-use-current]'),
        goParent: modal.querySelector('[data-go-parent]'),
        goRoot: modal.querySelector('[data-go-root]'),
        refresh: modal.querySelector('[data-refresh]'),
        pathInput: modal.querySelector('[data-path-input]'),
        pathGo: modal.querySelector('[data-path-go]'),
        loading: modal.querySelector('[data-loading]'),
      };

      if (refs.title) {
        refs.title.textContent = settings.title;
      }
      if (refs.description) {
        if (settings.description) {
          refs.description.textContent = settings.description;
          refs.description.classList.remove('hidden');
        } else {
          refs.description.classList.add('hidden');
        }
      }
      if (refs.confirm) {
        refs.confirm.textContent = settings.confirmText;
      }
      if (refs.pathInput && settings.initialPath) {
        refs.pathInput.value = settings.initialPath;
      }
      if (settings.mode === 'file' && refs.useCurrent) {
        refs.useCurrent.classList.add('hidden');
        refs.useCurrent.disabled = true;
      }

      document.body.appendChild(fragment);
      if (window.feather && typeof window.feather.replace === 'function') {
        window.feather.replace();
      }
      requestAnimationFrame(() => modal.classList.add('active'));

      const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      const state = {
        mode: settings.mode,
        selected: null,
        activeRow: null,
        listing: null,
        abortController: null,
        destroyRequested: false,
      };

      const focusableSelector = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

      function cleanup(result) {
        if (state.destroyRequested) {
          return;
        }
        state.destroyRequested = true;
        if (state.abortController) {
          try {
            state.abortController.abort();
          } catch (_) {
            // ignore
          }
        }
        modal.removeEventListener('keydown', handleKeydown);
        modal.classList.remove('active');
        setTimeout(() => {
          if (modal.parentNode) {
            modal.parentNode.removeChild(modal);
          }
          if (previouslyFocused) {
            try {
              previouslyFocused.focus();
            } catch (_) {
              // ignore focus failures
            }
          }
          resolve(result || null);
        }, 180);
      }

      function toggleLoading(loading) {
        if (refs.loading) {
          refs.loading.classList.toggle('hidden', !loading);
        }
        modal.setAttribute('aria-busy', loading ? 'true' : 'false');
        if (refs.confirm) {
          refs.confirm.disabled = loading || !state.selected;
        }
        if (refs.pathInput) {
          refs.pathInput.readOnly = loading;
        }
        if (refs.pathGo) {
          refs.pathGo.disabled = loading;
        }
      }

      function showError(message) {
        if (!refs.error) {
          return;
        }
        refs.error.textContent = message;
        refs.error.classList.remove('hidden');
      }

      function clearError() {
        if (!refs.error) {
          return;
        }
        refs.error.textContent = '';
        refs.error.classList.add('hidden');
      }

      function updateSelectionDisplay() {
        if (refs.selectedPath) {
          if (state.selected && state.selected.fullPath) {
            refs.selectedPath.textContent = state.selected.fullPath;
          } else {
            refs.selectedPath.textContent = settings.emptySelectionLabel;
          }
        }
        if (refs.confirm) {
          const busy = modal.getAttribute('aria-busy') === 'true';
          refs.confirm.disabled = busy || !state.selected;
        }
      }

      function setSelection(entry, row) {
        if (state.activeRow && state.activeRow !== row) {
          state.activeRow.classList.remove('is-selected');
        }
        if (row && entry) {
          row.classList.add('is-selected');
          state.activeRow = row;
        } else if (!entry) {
          state.activeRow = null;
        }
        state.selected = entry
          ? {
              name: entry.name,
              fullPath: entry.fullPath,
              relPath: entry.relPath,
              isDir: !!entry.isDir,
              selectable: entry.selectable !== false,
            }
          : null;
        updateSelectionDisplay();
      }

      function ensureRowVisible(row) {
        if (!row || !refs.entries) {
          return;
        }
        const containerRect = refs.entries.getBoundingClientRect();
        const rowRect = row.getBoundingClientRect();
        if (rowRect.top < containerRect.top || rowRect.bottom > containerRect.bottom) {
          row.scrollIntoView({ block: 'nearest' });
        }
      }

      function confirmSelection() {
        if (!state.selected || !state.selected.fullPath) {
          return;
        }
        cleanup(state.selected.fullPath);
      }

      function handleKeydown(evt) {
        if (evt.key === 'Escape') {
          evt.preventDefault();
          cleanup(null);
          return;
        }
        if (evt.key !== 'Tab') {
          return;
        }
        const focusable = Array.from(modal.querySelectorAll(focusableSelector)).filter((el) => !el.disabled && el.offsetParent !== null);
        if (focusable.length === 0) {
          evt.preventDefault();
          return;
        }
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (evt.shiftKey) {
          if (document.activeElement === first) {
            evt.preventDefault();
            last.focus();
          }
        } else if (document.activeElement === last) {
          evt.preventDefault();
          first.focus();
        }
      }

      function renderBreadcrumbs(items) {
        if (!refs.breadcrumbs) {
          return;
        }
        refs.breadcrumbs.innerHTML = '';
        if (!Array.isArray(items) || items.length === 0) {
          return;
        }
        const frag = document.createDocumentFragment();
        items.forEach((crumb, index) => {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.dataset.rel = crumb && typeof crumb.relPath === 'string' ? crumb.relPath : '';
          btn.textContent = crumb && crumb.label ? crumb.label : index === 0 ? 'Root' : '';
          frag.appendChild(btn);
        });
        refs.breadcrumbs.appendChild(frag);
      }

      function buildEntryRow(entry) {
        const row = document.createElement('button');
        row.type = 'button';
        row.className = 'path-entry';
        row.dataset.relPath = entry.relPath || '';
        row.dataset.type = entry.isDir ? 'dir' : 'file';
        if (!entry.selectable && !entry.isDir) {
          row.setAttribute('aria-disabled', 'true');
        }

        const nameEl = document.createElement('span');
        nameEl.className = 'name';
        nameEl.textContent = entry.name;

        const metaEl = document.createElement('div');
        metaEl.className = 'meta';
        const badge = document.createElement('span');
        badge.className = 'badge';
        badge.textContent = entry.isSymlink ? 'SYMLINK' : entry.isDir ? 'DIR' : 'FILE';
        metaEl.appendChild(badge);

        if (!entry.isDir) {
          const sizeLabel = document.createElement('span');
          sizeLabel.textContent = formatSize(entry.size);
          if (sizeLabel.textContent) {
            metaEl.appendChild(sizeLabel);
          }
        }

        const relLabel = document.createElement('span');
        relLabel.textContent = entry.relPath || '/';
        metaEl.appendChild(relLabel);

        if (entry.modifiedAt) {
          const modLabel = document.createElement('span');
          modLabel.textContent = formatTimestamp(entry.modifiedAt);
          if (modLabel.textContent) {
            metaEl.appendChild(modLabel);
          }
        }

        row.appendChild(nameEl);
        row.appendChild(metaEl);

        const entryData = {
          name: entry.name,
          fullPath: entry.fullPath,
          relPath: entry.relPath || '',
          isDir: !!entry.isDir,
          selectable: !!entry.selectable,
        };
        row.__entryData = entryData;

        row.addEventListener('click', (evt) => {
          evt.preventDefault();
          if (!entryData.selectable) {
            if (entryData.isDir) {
              requestListing(entryData.relPath);
            }
            return;
          }
          setSelection(entryData, row);
        });

        row.addEventListener('dblclick', (evt) => {
          evt.preventDefault();
          if (entryData.isDir) {
            requestListing(entryData.relPath);
            return;
          }
          if (entryData.selectable) {
            setSelection(entryData, row);
            confirmSelection();
          }
        });

        return row;
      }

      function renderListing(payload) {
        const data = payload || {};
        state.listing = data;
        clearError();
        renderBreadcrumbs(Array.isArray(data.breadcrumbs) ? data.breadcrumbs : []);

        if (refs.currentPath) {
          refs.currentPath.textContent = data.currentPath || data.rootPath || '';
        }
        if (refs.pathInput) {
          refs.pathInput.value = data.currentRelPath || '';
        }
        if (refs.goParent) {
          refs.goParent.disabled = !data.parentRelPath;
        }
        if (refs.goRoot) {
          refs.goRoot.disabled = !data.currentRelPath;
        }
        if (refs.useCurrent) {
          const canUseCurrent = !!(data.canSelectCurrent && state.mode !== 'file');
          refs.useCurrent.classList.toggle('hidden', !canUseCurrent);
          refs.useCurrent.disabled = !canUseCurrent;
        }

        const entries = Array.isArray(data.entries) ? data.entries : [];
        const frag = document.createDocumentFragment();
        if (refs.entries) {
          refs.entries.innerHTML = '';
        }
        if (refs.emptyState) {
          refs.emptyState.classList.toggle('hidden', entries.length !== 0);
        }

        entries.forEach((entry) => {
          const row = buildEntryRow(entry);
          frag.appendChild(row);
        });
        if (refs.entries) {
          refs.entries.appendChild(frag);
          if (data.limitHit) {
            const note = document.createElement('div');
            note.className = 'path-picker-note';
            note.textContent = 'Showing the first 750 items. Narrow your selection to view more results.';
            refs.entries.appendChild(note);
          }
        }

        const previousSelection = state.selected ? state.selected.fullPath : '';
        setSelection(null, null);

        if (previousSelection && refs.entries) {
          const match = Array.from(refs.entries.querySelectorAll('.path-entry')).find(
            (row) => row.__entryData && row.__entryData.fullPath === previousSelection,
          );
          if (match && match.__entryData) {
            setSelection(match.__entryData, match);
            ensureRowVisible(match);
            return;
          }
        }

        if (data.preselectRelPath && refs.entries) {
          const highlight = Array.from(refs.entries.querySelectorAll('.path-entry')).find(
            (row) => row.__entryData && row.__entryData.relPath === data.preselectRelPath,
          );
          if (highlight && highlight.__entryData) {
            setSelection(highlight.__entryData, highlight);
            ensureRowVisible(highlight);
            return;
          }
        }

        if (data.canSelectCurrent && state.mode !== 'file') {
          setSelection(
            {
              name: data.currentPath,
              fullPath: data.currentPath,
              relPath: data.currentRelPath || '',
              isDir: true,
              selectable: true,
            },
            null,
          );
        } else {
          updateSelectionDisplay();
        }
      }

      function requestListing(pathValue) {
        clearError();
        toggleLoading(true);
        if (state.abortController) {
          try {
            state.abortController.abort();
          } catch (_) {
            // ignore
          }
        }
        const controller = new AbortController();
        state.abortController = controller;

        const params = new URLSearchParams();
        params.set('mode', state.mode);
        if (typeof pathValue === 'string' && pathValue.trim() !== '') {
          params.set('path', pathValue.trim());
        }

        fetch(`/api/paths/browse?${params.toString()}`, {
          credentials: 'same-origin',
          signal: controller.signal,
        })
          .then(async (response) => {
            if (response.ok) {
              return response.json();
            }
            let message = 'Unable to browse path.';
            try {
              const body = await response.json();
              if (body && typeof body.error === 'string') {
                message = body.error;
              }
            } catch (_) {
              // ignore JSON parse errors
            }
            throw new Error(message);
          })
          .then((data) => {
            renderListing(data);
          })
          .catch((err) => {
            if (err.name === 'AbortError') {
              return;
            }
            showError(err.message || 'Unable to browse path.');
            if (window.showToast) {
              window.showToast('Path Picker', err.message || 'Unable to browse path.', 'danger');
            }
          })
          .finally(() => {
            toggleLoading(false);
          });
      }

      function handleManualNavigate() {
        if (!refs.pathInput) {
          return;
        }
        const value = refs.pathInput.value.trim();
        requestListing(value);
      }

      if (refs.cancel) {
        refs.cancel.addEventListener('click', () => cleanup(null));
      }
      if (refs.close) {
        refs.close.addEventListener('click', () => cleanup(null));
      }
      modal.addEventListener('click', (evt) => {
        if (evt.target === modal) {
          cleanup(null);
        }
      });
      modal.addEventListener('keydown', handleKeydown);

      if (refs.confirm) {
        refs.confirm.addEventListener('click', () => confirmSelection());
      }
      if (refs.pathGo) {
        refs.pathGo.addEventListener('click', handleManualNavigate);
      }
      if (refs.pathInput) {
        refs.pathInput.addEventListener('keydown', (evt) => {
          if (evt.key === 'Enter') {
            evt.preventDefault();
            handleManualNavigate();
          }
        });
      }

      if (refs.breadcrumbs) {
        refs.breadcrumbs.addEventListener('click', (evt) => {
          const btn = evt.target.closest('button[data-rel]');
          if (!btn) {
            return;
          }
          evt.preventDefault();
          requestListing(btn.dataset.rel || '');
        });
      }

      if (refs.goParent) {
        refs.goParent.addEventListener('click', () => {
          if (!state.listing || typeof state.listing.parentRelPath === 'undefined') {
            return;
          }
          requestListing(state.listing.parentRelPath);
        });
      }

      if (refs.goRoot) {
        refs.goRoot.addEventListener('click', () => requestListing(''));
      }
      if (refs.refresh) {
        refs.refresh.addEventListener('click', () => {
          const rel = state.listing ? state.listing.currentRelPath : '';
          requestListing(rel || '');
        });
      }

      if (refs.useCurrent) {
        refs.useCurrent.addEventListener('click', () => {
          if (!state.listing || !state.listing.canSelectCurrent || state.mode === 'file') {
            return;
          }
          setSelection(
            {
              name: state.listing.currentPath,
              fullPath: state.listing.currentPath,
              relPath: state.listing.currentRelPath || '',
              isDir: true,
              selectable: true,
            },
            null,
          );
          confirmSelection();
        });
      }

      requestListing(settings.initialPath || '');
    });
  }

  window.SDSM = window.SDSM || {};
  window.SDSM.pathPicker = {
    normalizeMode,
    open,
  };
})(window, document);
