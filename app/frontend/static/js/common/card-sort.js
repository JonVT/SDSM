/**
 * Card Sort – drag-and-drop card reordering with localStorage persistence.
 *
 * Usage: call SDSM.cardSort.init(scope) after the DOM (or a swapped region) is
 * ready.  Any element with the attribute `data-card-sortable` is treated as a
 * sortable container.  Direct children that have a `data-card-id` attribute
 * become draggable items.  The attribute value of `data-card-sortable` is used
 * as the localStorage key suffix (e.g. "server-1", "manager").
 */
(function (window) {
  'use strict';

  const STORAGE_PREFIX = 'sdsm-card-order:';

  // ----- persistence helpers ------------------------------------------------

  function loadOrder(key) {
    try {
      const raw = localStorage.getItem(STORAGE_PREFIX + key);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) return parsed;
      }
    } catch (_) { /* ignore */ }
    return null;
  }

  function saveOrder(key, ids) {
    try {
      localStorage.setItem(STORAGE_PREFIX + key, JSON.stringify(ids));
    } catch (_) { /* ignore */ }
  }

  // ----- DOM helpers --------------------------------------------------------

  function cardChildren(container) {
    return Array.from(container.children).filter(function (el) {
      return el.hasAttribute('data-card-id');
    });
  }

  function currentIds(container) {
    return cardChildren(container).map(function (el) {
      return el.getAttribute('data-card-id');
    });
  }

  // ----- apply saved order --------------------------------------------------

  function applyOrder(container, key) {
    var order = loadOrder(key);
    if (!order || !order.length) return;

    var cards = cardChildren(container);
    if (cards.length < 2) return;

    // Build a map of card-id → element
    var map = Object.create(null);
    cards.forEach(function (el) {
      map[el.getAttribute('data-card-id')] = el;
    });

    // Reorder according to saved order, appending unknowns at the end
    var placed = Object.create(null);
    order.forEach(function (id) {
      if (map[id]) {
        container.appendChild(map[id]);
        placed[id] = true;
      }
    });
    // Append any cards that weren't in the saved order (new cards)
    cards.forEach(function (el) {
      var id = el.getAttribute('data-card-id');
      if (!placed[id]) {
        container.appendChild(el);
      }
    });
  }

  // ----- drag & drop --------------------------------------------------------

  var dragState = {
    source: null,
    container: null,
    key: null,
    placeholder: null
  };

  function createPlaceholder(refEl) {
    var ph = document.createElement('div');
    ph.className = 'card-sort-placeholder';
    ph.style.height = refEl.offsetHeight + 'px';
    return ph;
  }

  function closestCard(el) {
    while (el && el !== document.body) {
      if (el.hasAttribute && el.hasAttribute('data-card-id') &&
          el.parentElement && el.parentElement.hasAttribute('data-card-sortable')) {
        return el;
      }
      el = el.parentElement;
    }
    return null;
  }

  function onDragStart(e) {
    var card = closestCard(e.target);
    if (!card) return;

    dragState.source = card;
    dragState.container = card.parentElement;
    dragState.key = dragState.container.getAttribute('data-card-sortable');

    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', card.getAttribute('data-card-id'));

    // Delay adding the class so the drag image renders first
    requestAnimationFrame(function () {
      card.classList.add('card-sort-dragging');
    });
  }

  function onDragOver(e) {
    if (!dragState.source) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';

    var container = dragState.container;
    var target = closestCard(e.target);
    if (!target || target === dragState.source || target.parentElement !== container) return;

    var rect = target.getBoundingClientRect();
    var midY = rect.top + rect.height / 2;

    if (e.clientY < midY) {
      container.insertBefore(dragState.source, target);
    } else {
      container.insertBefore(dragState.source, target.nextSibling);
    }
  }

  function onDragEnd(e) {
    if (!dragState.source) return;
    dragState.source.classList.remove('card-sort-dragging');

    if (dragState.container && dragState.key) {
      saveOrder(dragState.key, currentIds(dragState.container));
    }

    dragState.source = null;
    dragState.container = null;
    dragState.key = null;
  }

  // ----- touch fallback -----------------------------------------------------

  var touchState = {
    source: null,
    container: null,
    key: null,
    clone: null,
    offsetX: 0,
    offsetY: 0,
    scrollInterval: null
  };

  function onTouchStart(e) {
    var handle = e.target.closest('[data-card-drag-handle]');
    if (!handle) return;
    var card = closestCard(handle);
    if (!card) return;

    var container = card.parentElement;
    if (!container || !container.hasAttribute('data-card-sortable')) return;

    var touch = e.touches[0];
    var rect = card.getBoundingClientRect();

    touchState.source = card;
    touchState.container = container;
    touchState.key = container.getAttribute('data-card-sortable');
    touchState.offsetX = touch.clientX - rect.left;
    touchState.offsetY = touch.clientY - rect.top;

    // Create floating clone
    var clone = card.cloneNode(true);
    clone.className = 'card-sort-touch-clone';
    clone.style.width = rect.width + 'px';
    clone.style.left = rect.left + 'px';
    clone.style.top = rect.top + 'px';
    document.body.appendChild(clone);
    touchState.clone = clone;

    card.classList.add('card-sort-dragging');

    e.preventDefault();
  }

  function onTouchMove(e) {
    if (!touchState.source) return;
    e.preventDefault();

    var touch = e.touches[0];
    var clone = touchState.clone;
    if (clone) {
      clone.style.left = (touch.clientX - touchState.offsetX) + 'px';
      clone.style.top = (touch.clientY - touchState.offsetY) + 'px';
    }

    // Find the card under the touch point
    if (clone) clone.style.pointerEvents = 'none';
    var el = document.elementFromPoint(touch.clientX, touch.clientY);
    if (clone) clone.style.pointerEvents = '';

    var target = el ? closestCard(el) : null;
    if (!target || target === touchState.source || target.parentElement !== touchState.container) return;

    var rect = target.getBoundingClientRect();
    var midY = rect.top + rect.height / 2;

    if (touch.clientY < midY) {
      touchState.container.insertBefore(touchState.source, target);
    } else {
      touchState.container.insertBefore(touchState.source, target.nextSibling);
    }
  }

  function onTouchEnd() {
    if (!touchState.source) return;
    touchState.source.classList.remove('card-sort-dragging');

    if (touchState.clone) {
      touchState.clone.remove();
      touchState.clone = null;
    }

    if (touchState.container && touchState.key) {
      saveOrder(touchState.key, currentIds(touchState.container));
    }

    touchState.source = null;
    touchState.container = null;
    touchState.key = null;
  }

  // ----- initialisation -----------------------------------------------------

  var globalBound = false;

  function bindGlobal() {
    if (globalBound) return;
    globalBound = true;
    document.addEventListener('dragstart', onDragStart, false);
    document.addEventListener('dragover', onDragOver, false);
    document.addEventListener('dragend', onDragEnd, false);
    document.addEventListener('drop', function (e) {
      if (dragState.source) e.preventDefault();
    }, false);
    document.addEventListener('touchstart', onTouchStart, { passive: false });
    document.addEventListener('touchmove', onTouchMove, { passive: false });
    document.addEventListener('touchend', onTouchEnd, false);
    document.addEventListener('touchcancel', onTouchEnd, false);
  }

  function ensureHandle(card) {
    card.setAttribute('draggable', 'true');
  }

  function initContainer(container) {
    var key = container.getAttribute('data-card-sortable');
    var isNew = container.dataset.cardSortBound !== 'true';

    // Make direct card children draggable
    cardChildren(container).forEach(function (card) {
      ensureHandle(card);
    });

    if (isNew) {
      container.dataset.cardSortBound = 'true';
      // Apply saved order only on first init
      applyOrder(container, key);
    }
  }

  function init(scope) {
    bindGlobal();
    var root = scope instanceof Element || scope instanceof Document ? scope : document;
    var containers = root.querySelectorAll
      ? root.querySelectorAll('[data-card-sortable]')
      : [];

    // Also check if root itself is a sortable container
    if (root instanceof Element && root.hasAttribute && root.hasAttribute('data-card-sortable')) {
      initContainer(root);
    }
    for (var i = 0; i < containers.length; i++) {
      initContainer(containers[i]);
    }
    // If scope is a card inside a sortable container (or was swapped via
    // outerHTML and is now disconnected), ensure the live card gets draggable.
    if (root instanceof Element && root.hasAttribute('data-card-id')) {
      var liveCard = root;
      if (!root.isConnected) {
        var cardId = root.getAttribute('data-card-id');
        liveCard = cardId ? document.querySelector('[data-card-id="' + cardId + '"]') : null;
      }
      if (liveCard) {
        var parent = liveCard.parentElement;
        if (parent && parent.hasAttribute('data-card-sortable')) {
          ensureHandle(liveCard);
        }
      }
    }
  }

  // ----- expose on SDSM namespace -------------------------------------------

  if (!window.SDSM) window.SDSM = {};
  window.SDSM.cardSort = { init: init };

  // ----- auto-initialise ----------------------------------------------------

  // Self-init once the DOM is ready so draggable attrs and saved order are
  // applied on first paint.  Handles are now server-rendered in templates.
  function initAll() { init(document); }
  if (document.readyState !== 'loading') {
    initAll();
  } else {
    document.addEventListener('DOMContentLoaded', initAll, { once: true });
  }

  // Re-init whenever HTMX loads new content into the DOM.
  document.addEventListener('htmx:load', function (e) {
    var elt = e.detail ? e.detail.elt : null;
    if (elt) { init(elt); }
  });

})(window);
