(function(window) {
  if (!window || !window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard server card module aborting.');
    return;
  }

  const cardId = 'dashboard-server-tiles';
  const preferenceStore = new Map();
  const selectionSets = new WeakMap();

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
  const getSelectionSet = (card) => {
    if (!selectionSets.has(card)) {
      selectionSets.set(card, new Set());
    }
    return selectionSets.get(card);
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
      btn.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    });
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
    const summary = card.querySelector('[data-selected-count]');
    if (summary) {
      summary.textContent = `${count} selected`;
    }
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
    }
    const toggleBtn = card.querySelector('[data-select-toggle]');
    if (toggleBtn) {
      toggleBtn.setAttribute('aria-pressed', shouldEnable ? 'true' : 'false');
      const label = toggleBtn.querySelector('[data-select-toggle-label]');
      if (label) {
        label.textContent = shouldEnable ? 'Done Selecting' : 'Bulk Actions';
      }
    }
    syncSelection(card);
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
  };

  const clearSelection = (card) => {
    getSelectionSet(card).clear();
    syncSelection(card);
  };

  const runBulkAction = async (card, action) => {
    const ids = Array.from(getSelectionSet(card));
    if (!ids.length) {
      return;
    }
    const button = action === 'start' ? card.querySelector('[data-bulk-start]') : card.querySelector('[data-bulk-stop]');
    if (button) {
      button.disabled = true;
    }
    try {
      await apiRequest(`/api/servers/${action}-selected`, {
        method: 'POST',
        body: { ids }
      });
      clearSelection(card);
      toggleSelectionMode(card, false);
      requestGrid(card);
    } catch (err) {
      console.error(`Bulk ${action} failed`, err);
    } finally {
      if (button) {
        button.disabled = false;
      }
    }
  };

  const handleInlineActions = (card, event) => {
    const restartBtn = event.target.closest('[data-restart-server]');
    if (restartBtn) {
      event.preventDefault();
      event.stopPropagation();
      const serverId = restartBtn.getAttribute('data-restart-server');
      if (!serverId) return;
      restartBtn.disabled = true;
      apiRequest(`/api/servers/${serverId}/restart`, { method: 'POST', includeBodyWhenEmpty: true })
        .then(() => requestGrid(card))
        .catch(err => console.error('Restart failed', err))
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
      filterButtons.forEach(btn => {
        const handler = () => {
          const value = btn.dataset.filterValue || 'all';
          setFilterButtonState(card, value);
          persistState(card);
          requestGrid(card);
        };
        btn.addEventListener('click', handler);
        cleanup.push(() => btn.removeEventListener('click', handler));
      });
      const debouncedSearch = debounce((value) => {
        card.dataset.searchQuery = value.trim();
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
          persistState(card);
          requestGrid(card);
        };
        clearSearchBtn.addEventListener('click', handleClear);
        cleanup.push(() => clearSearchBtn.removeEventListener('click', handleClear));
      }

      attachGridInterceptor(card, getGrid(card), cleanup);

      const selectionToggle = card.querySelector('[data-select-toggle]');
      if (selectionToggle) {
        const handleToggle = () => toggleSelectionMode(card);
        selectionToggle.addEventListener('click', handleToggle);
        cleanup.push(() => selectionToggle.removeEventListener('click', handleToggle));
      }
      const selectAllBtn = card.querySelector('[data-select-all]');
      if (selectAllBtn) {
        const handleSelectAll = () => selectAllVisible(card);
        selectAllBtn.addEventListener('click', handleSelectAll);
        cleanup.push(() => selectAllBtn.removeEventListener('click', handleSelectAll));
      }
      const selectClearBtn = card.querySelector('[data-select-clear]');
      if (selectClearBtn) {
        const handleClearSelection = () => clearSelection(card);
        selectClearBtn.addEventListener('click', handleClearSelection);
        cleanup.push(() => selectClearBtn.removeEventListener('click', handleClearSelection));
      }
      const bulkStartBtn = card.querySelector('[data-bulk-start]');
      if (bulkStartBtn) {
        const handleBulkStart = () => runBulkAction(card, 'start');
        bulkStartBtn.addEventListener('click', handleBulkStart);
        cleanup.push(() => bulkStartBtn.removeEventListener('click', handleBulkStart));
      }
      const bulkStopBtn = card.querySelector('[data-bulk-stop]');
      if (bulkStopBtn) {
        const handleBulkStop = () => runBulkAction(card, 'stop');
        bulkStopBtn.addEventListener('click', handleBulkStop);
        cleanup.push(() => bulkStopBtn.removeEventListener('click', handleBulkStop));
      }

      const selectionChangeHandler = (event) => {
        const checkbox = event.target.closest('[data-server-select]');
        if (!checkbox) return;
        if (!card.classList.contains('is-selecting')) {
          checkbox.checked = false;
          return;
        }
        const id = parseInt(checkbox.value, 10);
        if (!Number.isInteger(id)) {
          checkbox.checked = false;
          return;
        }
        const set = getSelectionSet(card);
        if (checkbox.checked) {
          set.add(id);
        } else {
          set.delete(id);
        }
        const serverCard = checkbox.closest('.server-card');
        if (serverCard) {
          serverCard.classList.toggle('is-selected', checkbox.checked);
        }
        updateSelectionSummary(card);
      };
      card.addEventListener('change', selectionChangeHandler);
      cleanup.push(() => card.removeEventListener('change', selectionChangeHandler));

      const inlineActionHandler = (event) => handleInlineActions(card, event);
      card.addEventListener('click', inlineActionHandler);
      cleanup.push(() => card.removeEventListener('click', inlineActionHandler));

      const rebindNavigation = () => bindNavigation(card);
      const handleAfterSwap = (event) => {
        if (!event?.target || !card.contains(event.target)) {
          return;
        }
        if (event.target.id === 'server-grid') {
          rebindNavigation();
          syncSelection(card);
        }
      };
      requestAnimationFrame(() => {
        rebindNavigation();
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
