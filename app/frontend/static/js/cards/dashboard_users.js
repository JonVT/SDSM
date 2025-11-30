(function(window) {
  if (!window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard users card module aborting.');
    return;
  }

  const INTERACTIVE_SELECTOR = 'a, button, input, select, textarea, [contenteditable="true"], [data-card-nav-exempt], [data-card-refresh], [role="button"], [hx-post], [hx-get], [hx-put], [hx-delete]';

  const shouldIgnoreNavigation = (target, card) => {
    if (!target || typeof target.closest !== 'function') {
      return false;
    }
    const interactive = target.closest(INTERACTIVE_SELECTOR);
    if (!interactive) {
      return false;
    }
    if (card && interactive === card) {
      return false;
    }
    return true;
  };

  const triggerNavigation = (path) => {
    if (!path) {
      return;
    }
    const navLink = document.querySelector(`.nav-item[data-target="${path}"]`);
    if (navLink) {
      if (typeof navLink.focus === 'function') {
        try {
          navLink.focus({ preventScroll: true });
        } catch (_) {
          navLink.focus();
        }
      }
      navLink.click();
      return;
    }
    window.location.assign(path);
  };

  const bindCardNavigation = (card) => {
    if (!(card instanceof Element)) {
      return () => {};
    }
    const path = card.getAttribute('data-card-navigate');
    if (!path) {
      return () => {};
    }

    const handleClick = (event) => {
      if (event.defaultPrevented) {
        return;
      }
      if (event.button !== undefined && event.button !== 0) {
        return;
      }
      if (shouldIgnoreNavigation(event.target, card)) {
        return;
      }
      event.preventDefault();
      triggerNavigation(path);
    };

    const handleKeyDown = (event) => {
      if (event.defaultPrevented) {
        return;
      }
      const key = event.key;
      if (key !== 'Enter' && key !== ' ') {
        return;
      }
      if (shouldIgnoreNavigation(event.target, card)) {
        return;
      }
      event.preventDefault();
      triggerNavigation(path);
    };

    card.addEventListener('click', handleClick);
    card.addEventListener('keydown', handleKeyDown);

    return () => {
      card.removeEventListener('click', handleClick);
      card.removeEventListener('keydown', handleKeyDown);
    };
  };

  const module = {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }
      const refreshAction = () => {
        window.SDSM.cards.refresh('dashboard-users');
      };
      const refreshButton = card.querySelector('[data-card-refresh]');
      if (refreshButton) {
        refreshButton.addEventListener('click', refreshAction);
      }

      const unbindNavigation = bindCardNavigation(card);

      return () => {
        if (refreshButton) {
          refreshButton.removeEventListener('click', refreshAction);
        }
        unbindNavigation();
      };
    }
  };

  window.SDSM.cards.define('dashboard-users', module);
})(window);
