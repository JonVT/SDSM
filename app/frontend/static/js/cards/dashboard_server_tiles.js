(function(window) {
  if (!window || !window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard server card module aborting.');
    return;
  }

  const cardId = 'dashboard-server-tiles';
  const preferenceStore = new Map();
  const selectionSets = new WeakMap();
  const lastSelectionIndex = new WeakMap();
  const FOCUSABLE_SELECTOR = [
    'button:not([disabled])',
    '[href]',
    'input:not([type="hidden"]):not([disabled])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])'
  ].join(', ');

  const escapeRegExp = (value) => String(value || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const escapeHtml = (value) => String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

  const getHighlightTerms = (query) => {
    const raw = String(query || '').trim();
    if (!raw) {
      return [];
    }
    const parts = raw
      .split(/\s+/)
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0)
      .map((entry) => entry.toLowerCase());
    return Array.from(new Set(parts));
  };

  const applyHighlightsToElement = (element, terms) => {
    if (!(element instanceof Element)) {
      return;
    }
    const base = element.dataset.highlightBaseText ?? element.textContent ?? '';
    if (!element.dataset.highlightBaseText) {
      element.dataset.highlightBaseText = base;
    }
    if (!terms.length) {
      element.textContent = base;
      return;
    }
    const safeTerms = terms.map(escapeRegExp).filter(Boolean);
    if (!safeTerms.length) {
      element.textContent = base;
      return;
    }
    const regex = new RegExp(`(${safeTerms.join('|')})`, 'gi');
    const escapedBase = escapeHtml(base);
    const highlighted = escapedBase.replace(regex, '<mark class="search-match">$1</mark>');
    element.innerHTML = highlighted;
  };

  const applySearchHighlights = (card) => {
    if (!(card instanceof Element)) {
      return;
    }
    const terms = getHighlightTerms(card.dataset.searchQuery || '');
    card.querySelectorAll('[data-server-name], [data-world-value], [data-port-value]').forEach((element) => {
      applyHighlightsToElement(element, terms);
    });
  };

  const toTitleCase = (value) => String(value || '')
    .replace(/[-_]+/g, ' ')
    .replace(/\b\w/g, (m) => m.toUpperCase());

  const updateSearchSummary = (card) => {
    if (!(card instanceof Element)) {
      return;
    }
    const summary = card.querySelector('[data-search-results]');
    if (!summary) {
      return;
    }
    const query = (card.dataset.searchQuery || '').trim();
    const filter = (card.dataset.activeFilter || 'all').trim();
    const count = card.querySelectorAll('#server-grid .server-card').length;
    const noun = count === 1 ? 'server' : 'servers';

    if (query) {
      summary.textContent = `${count} ${noun} for "${query}"`;
      return;
    }
    if (filter && filter !== 'all') {
      summary.textContent = `${count} ${noun} in ${toTitleCase(filter)}`;
      return;
    }
    summary.textContent = `Showing ${count} ${noun}`;
  };

  const debounce = (fn, delay = 250) => {
    let timer;
    return (...args) => {
      clearTimeout(timer);
      timer = setTimeout(() => fn(...args), delay);
    };
  };

  const apiRequest = async (url, options = {}) => {
    if (window.SDSM?.api?.request) {
      return window.SDSM.api.request(url, options);
    }
    const { method = 'POST', body, includeBodyWhenEmpty = false } = options;
    const headers = {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      'HX-Request': 'true'
    };
    const response = await fetch(url, {
      method,
      headers,
      credentials: 'same-origin',
      body: body ? JSON.stringify(body) : (includeBodyWhenEmpty ? '{}' : undefined)
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || 'Request failed');
    }
    try {
      return await response.json();
    } catch (_) {
      return {};
    }
  };

  const getGrid = (card) => card.querySelector('#server-grid');
  const getCardKey = (card) => (card?.dataset?.cardId) || cardId;
  const announceSelection = (card, message) => {
    if (!(card instanceof Element) || !message) {
      return;
    }
    const liveRegion = card.querySelector('[data-selection-live]');
    if (!liveRegion) {
      return;
    }
    liveRegion.textContent = '';
    requestAnimationFrame(() => {
      liveRegion.textContent = message;
    });
  };
  const getSelectionSet = (card) => {
    if (!selectionSets.has(card)) {
      selectionSets.set(card, new Set());
    }
    return selectionSets.get(card);
  };

  const snapshotStatusPresentation = (serverCard) => {
    const badge = serverCard.querySelector('[data-status]');
    const uptime = serverCard.querySelector('[data-status-uptime]');
    return {
      badgeClass: badge ? badge.className : '',
      badgeText: badge ? badge.textContent : '',
      badgeAria: badge ? badge.getAttribute('aria-label') : '',
      badgeTitle: badge ? badge.getAttribute('title') : '',
      uptimeText: uptime ? uptime.textContent : '',
      uptimeMode: uptime ? (uptime.dataset.relativeMode || '') : '',
      uptimeSource: uptime ? (uptime.dataset.relativeSource || '') : '',
      uptimePrefix: uptime ? (uptime.dataset.relativePrefix || '') : '',
      uptimeSuffix: uptime ? (uptime.dataset.relativeSuffix || '') : ''
    };
  };

  const clearPendingAction = (serverCard, restore = true) => {
    if (!(serverCard instanceof Element)) {
      return;
    }
    const pending = serverCard.dataset.pendingAction || '';
    serverCard.classList.remove('is-pending-action');
    if (pending) {
      serverCard.classList.remove(`is-pending-${pending}`);
    }
    delete serverCard.dataset.pendingAction;

    const controls = serverCard.querySelectorAll('[data-server-action], .card-footer .btn');
    controls.forEach((button) => {
      if (button instanceof HTMLButtonElement) {
        button.disabled = false;
      }
      button.removeAttribute('aria-busy');
    });

    if (!restore) {
      delete serverCard.__pendingStatusSnapshot;
      return;
    }

    const snapshot = serverCard.__pendingStatusSnapshot;
    if (!snapshot) {
      return;
    }
    const badge = serverCard.querySelector('[data-status]');
    const uptime = serverCard.querySelector('[data-status-uptime]');
    const pendingTextByAction = {
      start: 'Starting...',
      stop: 'Stopping...',
      restart: 'Restarting...'
    };
    if (badge) {
      badge.className = snapshot.badgeClass || badge.className;
      badge.textContent = snapshot.badgeText || '';
      if (snapshot.badgeAria) badge.setAttribute('aria-label', snapshot.badgeAria); else badge.removeAttribute('aria-label');
      if (snapshot.badgeTitle) badge.setAttribute('title', snapshot.badgeTitle); else badge.removeAttribute('title');
    }
    if (uptime) {
      uptime.textContent = snapshot.uptimeText || '';
      if (snapshot.uptimeMode) uptime.dataset.relativeMode = snapshot.uptimeMode; else delete uptime.dataset.relativeMode;
      if (snapshot.uptimeSource) uptime.dataset.relativeSource = snapshot.uptimeSource; else delete uptime.dataset.relativeSource;
      if (snapshot.uptimePrefix) uptime.dataset.relativePrefix = snapshot.uptimePrefix; else delete uptime.dataset.relativePrefix;
      if (snapshot.uptimeSuffix) uptime.dataset.relativeSuffix = snapshot.uptimeSuffix; else delete uptime.dataset.relativeSuffix;
    }
    delete serverCard.__pendingStatusSnapshot;
  };

  const setPendingAction = (serverCard, action) => {
    if (!(serverCard instanceof Element)) {
      return;
    }
    clearPendingAction(serverCard, false);
    serverCard.__pendingStatusSnapshot = snapshotStatusPresentation(serverCard);
    const normalized = String(action || '').trim().toLowerCase();
    serverCard.dataset.pendingAction = normalized;
    serverCard.classList.add('is-pending-action');
    if (normalized) {
      serverCard.classList.add(`is-pending-${normalized}`);
    }

    const badge = serverCard.querySelector('[data-status]');
    const uptime = serverCard.querySelector('[data-status-uptime]');
    if (badge) {
      badge.classList.remove('is-running', 'is-stopped', 'is-paused', 'is-starting', 'is-stopping', 'is-error');
      if (normalized === 'start') {
        badge.classList.add('is-starting');
        badge.textContent = 'Starting';
      } else if (normalized === 'stop') {
        badge.classList.add('is-stopping');
        badge.textContent = 'Stopping';
      } else if (normalized === 'restart') {
        badge.classList.add('is-starting');
        badge.textContent = 'Restarting';
      }
      badge.setAttribute('aria-label', `Status: ${badge.textContent}`);
      badge.setAttribute('title', badge.textContent);
    }
    if (uptime) {
      delete uptime.dataset.relativeMode;
      delete uptime.dataset.relativeSource;
      delete uptime.dataset.relativePrefix;
      delete uptime.dataset.relativeSuffix;
      uptime.textContent = pendingTextByAction[normalized] || 'Working...';
    }

    const controls = serverCard.querySelectorAll('[data-server-action], .card-footer .btn');
    controls.forEach((button) => {
      if (button instanceof HTMLButtonElement) {
        button.disabled = true;
      }
      button.setAttribute('aria-busy', 'true');
    });
  };

  const updateSearchVisual = (wrapper, value) => {
    if (!wrapper) return;
    if (value && value.trim().length) {
      wrapper.classList.add('has-value');
    } else {
      wrapper.classList.remove('has-value');
    }
  };

  const persistState = (card) => {
    preferenceStore.set(getCardKey(card), {
      filter: card.dataset.activeFilter || 'all',
      search: card.dataset.searchQuery || ''
    });
  };

  const restoreState = (card, searchInput, searchWrapper) => {
    const saved = preferenceStore.get(getCardKey(card));
    if (!saved) {
      persistState(card);
      return false;
    }
    card.dataset.activeFilter = saved.filter || 'all';
    card.dataset.searchQuery = saved.search || '';
    if (searchInput) {
      searchInput.value = saved.search || '';
      updateSearchVisual(searchWrapper, saved.search);
    }
    return Boolean((saved.filter && saved.filter !== 'all') || (saved.search && saved.search.length));
  };

  const bindNavigation = (card) => {
    if (!window.SDSM?.ui?.bindServerCardNavigation) {
      return;
    }
    const grid = getGrid(card);
    if (grid) {
      window.SDSM.ui.bindServerCardNavigation(grid);
    }
  };

  const setFilterButtonState = (card, filter) => {
    const normalized = filter && filter.trim() ? filter.trim() : 'all';
    card.dataset.activeFilter = normalized;
    card.querySelectorAll('[data-filter-value]').forEach(btn => {
      const isActive = btn.dataset.filterValue === normalized;
      btn.classList.toggle('is-active', isActive);
      btn.setAttribute('role', 'tab');
      btn.setAttribute('aria-selected', isActive ? 'true' : 'false');
      btn.setAttribute('tabindex', isActive ? '0' : '-1');
    });
  };

  const isEditableTarget = (target) => {
    if (!(target instanceof Element)) {
      return false;
    }
    if (target.isContentEditable) {
      return true;
    }
    return !!target.closest('input, textarea, select, [contenteditable="true"]');
  };

  const requestGrid = (card) => {
    const grid = getGrid(card);
    if (!grid || !window.htmx) {
      return;
    }
    const url = grid.getAttribute('hx-get') || '/api/servers';
    const values = {};
    const filter = card.dataset.activeFilter;
    if (filter && filter !== 'all') {
      values.filter = filter;
    }
    const search = (card.dataset.searchQuery || '').trim();
    if (search.length) {
      values.search = search;
    }
    window.htmx.ajax('GET', url, {
      target: grid,
      swap: 'innerHTML',
      values
    });
  };

  const attachGridInterceptor = (card, grid, cleanup) => {
    if (!grid) {
      return;
    }
    const handler = (event) => {
      const params = event?.detail?.parameters;
      if (!params) return;
      const filter = card.dataset.activeFilter;
      if (filter && filter !== 'all') {
        params.filter = filter;
      } else {
        delete params.filter;
      }
      const search = (card.dataset.searchQuery || '').trim();
      if (search.length) {
        params.search = search;
      } else {
        delete params.search;
      }
    };
    grid.addEventListener('htmx:configRequest', handler);
    cleanup.push(() => grid.removeEventListener('htmx:configRequest', handler));
  };

  const updateSelectionSummary = (card) => {
    const active = card.classList.contains('is-selecting');
    const set = getSelectionSet(card);
    const count = active ? set.size : 0;
    card.classList.toggle('has-selection', active && count > 0);
    card.querySelectorAll('[data-selected-count]').forEach(summary => {
      summary.textContent = `${count} selected`;
    });
    card.querySelectorAll('[data-bulk-start], [data-bulk-stop]').forEach(btn => {
      btn.disabled = count === 0;
    });
  };

  const syncSelection = (card) => {
    const active = card.classList.contains('is-selecting');
    const set = getSelectionSet(card);
    card.querySelectorAll('.server-card').forEach(serverCard => {
      const id = parseInt(serverCard.dataset.serverId, 10);
      const checkbox = serverCard.querySelector('[data-server-select]');
      const isSelected = active && Number.isInteger(id) && set.has(id);
      serverCard.classList.toggle('is-selected', isSelected);
      if (checkbox) {
        checkbox.checked = !!isSelected;
        checkbox.tabIndex = active ? 0 : -1;
      }
    });
    updateSelectionSummary(card);
  };

  const toggleSelectionMode = (card, forceState) => {
    const shouldEnable = typeof forceState === 'boolean' ? forceState : !card.classList.contains('is-selecting');
    card.classList.toggle('is-selecting', shouldEnable);
    if (!shouldEnable) {
      getSelectionSet(card).clear();
      lastSelectionIndex.delete(card);
    }
    card.querySelectorAll('[data-select-toggle]').forEach((toggleBtn) => {
      toggleBtn.setAttribute('aria-pressed', shouldEnable ? 'true' : 'false');
      toggleBtn.setAttribute('aria-expanded', shouldEnable ? 'true' : 'false');
      toggleBtn.setAttribute('aria-label', shouldEnable ? 'Disable bulk selection mode' : 'Enable bulk selection mode');
      const label = toggleBtn.querySelector('[data-select-toggle-label]');
      if (label) {
        label.textContent = shouldEnable ? 'Done Selecting' : 'Bulk Actions';
      }
    });
    syncSelection(card);
    announceSelection(card, shouldEnable ? 'Bulk selection mode enabled.' : 'Bulk selection mode disabled.');
    if (shouldEnable) {
      requestAnimationFrame(() => focusFirstSelectionControl(card));
    }
  };

  const selectAllVisible = (card) => {
    if (!card.classList.contains('is-selecting')) {
      toggleSelectionMode(card, true);
    }
    const set = getSelectionSet(card);
    card.querySelectorAll('.server-card').forEach(serverCard => {
      const id = parseInt(serverCard.dataset.serverId, 10);
      if (Number.isInteger(id)) {
        set.add(id);
      }
    });
    syncSelection(card);
    announceSelection(card, `${set.size} servers selected.`);
  };

  const clearSelection = (card) => {
    getSelectionSet(card).clear();
    lastSelectionIndex.delete(card);
    syncSelection(card);
    announceSelection(card, 'Selection cleared.');
  };

  const isElementVisible = (element) => {
    if (!(element instanceof Element)) {
      return false;
    }
    return element.getClientRects().length > 0;
  };

  const getSelectionFocusableElements = (card) => {
    if (!(card instanceof Element)) {
      return [];
    }
    const scopes = [
      card.querySelector('[data-selection-panel]'),
      card.querySelector('[data-selection-sticky]')
    ].filter((scope) => scope instanceof Element && isElementVisible(scope));

    const seen = new Set();
    const focusables = [];
    scopes.forEach((scope) => {
      scope.querySelectorAll(FOCUSABLE_SELECTOR).forEach((node) => {
        if (!(node instanceof HTMLElement) || seen.has(node)) {
          return;
        }
        seen.add(node);
        focusables.push(node);
      });
    });
    return focusables;
  };

  const focusFirstSelectionControl = (card) => {
    const preferred = card.querySelector('[data-selection-panel] [data-select-all]');
    if (preferred instanceof HTMLElement && isElementVisible(preferred)) {
      preferred.focus();
      return;
    }
    const [first] = getSelectionFocusableElements(card);
    if (first instanceof HTMLElement) {
      first.focus();
    }
  };

  const runBulkAction = async (card, action) => {
    const ids = Array.from(getSelectionSet(card));
    if (!ids.length) {
      return;
    }
    const buttons = action === 'start'
      ? Array.from(card.querySelectorAll('[data-bulk-start]'))
      : Array.from(card.querySelectorAll('[data-bulk-stop]'));
    buttons.forEach((button) => { button.disabled = true; });
    try {
      await apiRequest(`/api/servers/${action}-selected`, {
        method: 'POST',
        body: { ids }
      });
      announceSelection(card, `${action === 'start' ? 'Start' : 'Stop'} requested for ${ids.length} selected servers.`);
      clearSelection(card);
      toggleSelectionMode(card, false);
      requestGrid(card);
    } catch (err) {
      console.error(`Bulk ${action} failed`, err);
    } finally {
      buttons.forEach((button) => { button.disabled = false; });
    }
  };

  const handleInlineActions = (card, event) => {
    const actionBtn = event.target.closest('[data-server-action]');
    if (actionBtn) {
      const serverCard = actionBtn.closest('.server-card');
      const action = actionBtn.dataset.serverAction;
      if (serverCard && action && (action === 'start' || action === 'stop')) {
        setPendingAction(serverCard, action);
      }
    }

    const restartBtn = event.target.closest('[data-restart-server]');
    if (restartBtn) {
      event.preventDefault();
      event.stopPropagation();
      const serverId = restartBtn.getAttribute('data-restart-server');
      if (!serverId) return;
      const serverCard = restartBtn.closest('.server-card');
      if (serverCard) {
        setPendingAction(serverCard, 'restart');
      }
      restartBtn.disabled = true;
      apiRequest(`/api/servers/${serverId}/restart`, { method: 'POST', includeBodyWhenEmpty: true })
        .then(() => requestGrid(card))
        .catch(err => {
          console.error('Restart failed', err);
          if (serverCard) {
            clearPendingAction(serverCard, true);
          }
        })
        .finally(() => { restartBtn.disabled = false; });
      return;
    }
    const logsBtn = event.target.closest('[data-server-logs]');
    if (logsBtn) {
      event.preventDefault();
      event.stopPropagation();
      const target = logsBtn.getAttribute('data-server-logs');
      if (target) {
        window.location.href = target;
      }
    }
  };

  window.SDSM.cards.define(cardId, {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }

      const cleanup = [];
      const refreshButton = card.querySelector('[data-card-refresh]');
      const handleRefresh = () => window.SDSM.cards.refresh(cardId);
      if (refreshButton) {
        refreshButton.addEventListener('click', handleRefresh);
        cleanup.push(() => refreshButton.removeEventListener('click', handleRefresh));
      }

      const searchWrapper = card.querySelector('.server-search');
      const searchInput = card.querySelector('[data-server-search]');
      const clearSearchBtn = card.querySelector('[data-clear-search]');
      const needsInitialReload = restoreState(card, searchInput, searchWrapper);
      setFilterButtonState(card, card.dataset.activeFilter || 'all');

      const filterButtons = card.querySelectorAll('[data-filter-value]');
      const filterButtonsArray = Array.from(filterButtons);
      filterButtons.forEach(btn => {
        const handler = () => {
          const value = btn.dataset.filterValue || 'all';
          setFilterButtonState(card, value);
          persistState(card);
          requestGrid(card);
        };
        btn.addEventListener('click', handler);
        cleanup.push(() => btn.removeEventListener('click', handler));

        const keyHandler = (event) => {
          const currentIndex = filterButtonsArray.indexOf(btn);
          if (currentIndex < 0) return;

          let nextIndex = -1;
          switch (event.key) {
            case 'Enter':
            case ' ':
              event.preventDefault();
              btn.click();
              return;
            case 'ArrowLeft':
            case 'ArrowUp':
              nextIndex = (currentIndex - 1 + filterButtonsArray.length) % filterButtonsArray.length;
              break;
            case 'ArrowRight':
            case 'ArrowDown':
              nextIndex = (currentIndex + 1) % filterButtonsArray.length;
              break;
            case 'Home':
              nextIndex = 0;
              break;
            case 'End':
              nextIndex = filterButtonsArray.length - 1;
              break;
            default:
              return;
          }

          event.preventDefault();
          const nextBtn = filterButtonsArray[nextIndex];
          if (!nextBtn) return;
          nextBtn.focus();
          nextBtn.click();
        };

        btn.addEventListener('keydown', keyHandler);
        cleanup.push(() => btn.removeEventListener('keydown', keyHandler));
      });
      const debouncedSearch = debounce((value) => {
        card.dataset.searchQuery = value.trim();
        applySearchHighlights(card);
        updateSearchSummary(card);
        persistState(card);
        requestGrid(card);
      }, 350);
      if (searchInput) {
        const handleInput = (event) => {
          updateSearchVisual(searchWrapper, event.target.value);
          debouncedSearch(event.target.value || '');
        };
        const handleKey = (event) => {
          if (event.key === 'Escape') {
            searchInput.value = '';
            updateSearchVisual(searchWrapper, '');
            card.dataset.searchQuery = '';
            applySearchHighlights(card);
            updateSearchSummary(card);
            persistState(card);
            requestGrid(card);
            searchInput.blur();
          }
        };
        searchInput.addEventListener('input', handleInput);
        searchInput.addEventListener('keydown', handleKey);
        cleanup.push(() => {
          searchInput.removeEventListener('input', handleInput);
          searchInput.removeEventListener('keydown', handleKey);
        });
      }
      if (clearSearchBtn) {
        const handleClear = () => {
          if (searchInput) {
            searchInput.value = '';
          }
          card.dataset.searchQuery = '';
          updateSearchVisual(searchWrapper, '');
          applySearchHighlights(card);
          updateSearchSummary(card);
          persistState(card);
          requestGrid(card);
        };
        clearSearchBtn.addEventListener('click', handleClear);
        cleanup.push(() => clearSearchBtn.removeEventListener('click', handleClear));
      }

      attachGridInterceptor(card, getGrid(card), cleanup);

      card.querySelectorAll('[data-select-toggle]').forEach((selectionToggle) => {
        const handleToggle = () => toggleSelectionMode(card);
        selectionToggle.addEventListener('click', handleToggle);
        cleanup.push(() => selectionToggle.removeEventListener('click', handleToggle));
      });
      card.querySelectorAll('[data-select-all]').forEach((selectAllBtn) => {
        const handleSelectAll = () => selectAllVisible(card);
        selectAllBtn.addEventListener('click', handleSelectAll);
        cleanup.push(() => selectAllBtn.removeEventListener('click', handleSelectAll));
      });
      card.querySelectorAll('[data-select-clear]').forEach((selectClearBtn) => {
        const handleClearSelection = () => clearSelection(card);
        selectClearBtn.addEventListener('click', handleClearSelection);
        cleanup.push(() => selectClearBtn.removeEventListener('click', handleClearSelection));
      });
      card.querySelectorAll('[data-bulk-start]').forEach((bulkStartBtn) => {
        const handleBulkStart = () => runBulkAction(card, 'start');
        bulkStartBtn.addEventListener('click', handleBulkStart);
        cleanup.push(() => bulkStartBtn.removeEventListener('click', handleBulkStart));
      });
      card.querySelectorAll('[data-bulk-stop]').forEach((bulkStopBtn) => {
        const handleBulkStop = () => runBulkAction(card, 'stop');
        bulkStopBtn.addEventListener('click', handleBulkStop);
        cleanup.push(() => bulkStopBtn.removeEventListener('click', handleBulkStop));
      });

      const selectionChangeHandler = (event) => {
        const checkbox = event.target.closest('[data-server-select]');
        if (!checkbox) return;
        if (!card.classList.contains('is-selecting')) {
          toggleSelectionMode(card, true);
        }
        const id = parseInt(checkbox.value, 10);
        if (!Number.isInteger(id)) {
          checkbox.checked = false;
          return;
        }

        const visibleCards = Array.from(card.querySelectorAll('.server-card'));
        const currentCard = checkbox.closest('.server-card');
        const currentIndex = currentCard ? visibleCards.indexOf(currentCard) : -1;

        const set = getSelectionSet(card);

        const applyToCard = (serverCardEl, shouldCheck) => {
          if (!(serverCardEl instanceof Element)) return;
          const cb = serverCardEl.querySelector('[data-server-select]');
          const sid = parseInt(serverCardEl.dataset.serverId || '', 10);
          if (!(cb instanceof HTMLInputElement) || !Number.isInteger(sid)) return;
          cb.checked = shouldCheck;
          if (shouldCheck) {
            set.add(sid);
          } else {
            set.delete(sid);
          }
          serverCardEl.classList.toggle('is-selected', shouldCheck);
        };

        const previousIndex = lastSelectionIndex.has(card) ? lastSelectionIndex.get(card) : -1;
        const isShiftRange = event.shiftKey && currentIndex >= 0 && previousIndex >= 0;

        if (isShiftRange) {
          const start = Math.min(previousIndex, currentIndex);
          const end = Math.max(previousIndex, currentIndex);
          for (let idx = start; idx <= end; idx += 1) {
            applyToCard(visibleCards[idx], checkbox.checked);
          }
        } else {
          if (checkbox.checked) {
            set.add(id);
          } else {
            set.delete(id);
          }
          const serverCard = checkbox.closest('.server-card');
          if (serverCard) {
            serverCard.classList.toggle('is-selected', checkbox.checked);
          }
        }

        if (currentIndex >= 0) {
          lastSelectionIndex.set(card, currentIndex);
        }

        updateSelectionSummary(card);
        const selectedCount = getSelectionSet(card).size;
        announceSelection(card, `${selectedCount} ${selectedCount === 1 ? 'server' : 'servers'} selected.`);
      };
      card.addEventListener('change', selectionChangeHandler);
      cleanup.push(() => card.removeEventListener('change', selectionChangeHandler));

      const clearPendingOnRequestError = (event) => {
        const source = event?.detail?.elt instanceof Element ? event.detail.elt : null;
        const failedCard = source ? source.closest('.server-card') : null;
        if (!failedCard) {
          return;
        }
        clearPendingAction(failedCard, true);
      };
      card.addEventListener('htmx:responseError', clearPendingOnRequestError);
      card.addEventListener('htmx:sendError', clearPendingOnRequestError);
      cleanup.push(() => card.removeEventListener('htmx:responseError', clearPendingOnRequestError));
      cleanup.push(() => card.removeEventListener('htmx:sendError', clearPendingOnRequestError));

      const inlineActionHandler = (event) => handleInlineActions(card, event);
      card.addEventListener('click', inlineActionHandler);
      cleanup.push(() => card.removeEventListener('click', inlineActionHandler));

      const selectionShortcutHandler = (event) => {
        if (!(event instanceof KeyboardEvent)) {
          return;
        }
        const inSelectionMode = card.classList.contains('is-selecting');
        if (!inSelectionMode) {
          return;
        }
        if (isEditableTarget(event.target)) {
          return;
        }

        if (event.key === 'Tab') {
          const focusables = getSelectionFocusableElements(card);
          if (!focusables.length) {
            return;
          }
          const first = focusables[0];
          const last = focusables[focusables.length - 1];
          const active = document.activeElement;
          const focusInsideControls = active instanceof Element && focusables.includes(active);

          if (event.shiftKey) {
            if (!focusInsideControls || active === first) {
              event.preventDefault();
              last.focus();
            }
          } else if (!focusInsideControls || active === last) {
            event.preventDefault();
            first.focus();
          }
          return;
        }

        if (event.key === 'Escape') {
          event.preventDefault();
          toggleSelectionMode(card, false);
          const toggle = card.querySelector('[data-select-toggle]');
          if (toggle instanceof HTMLElement) {
            toggle.focus();
          }
          return;
        }

        const lowerKey = String(event.key || '').toLowerCase();
        const isSelectAllShortcut = lowerKey === 'a' && (event.shiftKey || event.ctrlKey || event.metaKey);
        if (isSelectAllShortcut) {
          event.preventDefault();
          selectAllVisible(card);
        }
      };
      card.addEventListener('keydown', selectionShortcutHandler);
      cleanup.push(() => card.removeEventListener('keydown', selectionShortcutHandler));

      const rebindNavigation = () => bindNavigation(card);
      const handleAfterSwap = (event) => {
        if (!event?.target || !card.contains(event.target)) {
          return;
        }
        if (event.target.id === 'server-grid') {
          rebindNavigation();
          applySearchHighlights(card);
          updateSearchSummary(card);
          syncSelection(card);
        }
      };
      requestAnimationFrame(() => {
        rebindNavigation();
        applySearchHighlights(card);
        updateSearchSummary(card);
        syncSelection(card);
      });
      card.addEventListener('htmx:afterSwap', handleAfterSwap);
      cleanup.push(() => card.removeEventListener('htmx:afterSwap', handleAfterSwap));

      updateSelectionSummary(card);
      if (needsInitialReload) {
        requestGrid(card);
      }

      persistState(card);

      return () => {
        cleanup.forEach(fn => {
          try {
            fn();
          } catch (_) {}
        });
      };
    }
  });
})(window);
