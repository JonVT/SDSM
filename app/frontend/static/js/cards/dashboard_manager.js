(function(window) {
  if (!window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard manager card module aborting.');
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

  const bindCopyButtons = (card) => {
    const buttons = Array.from(card.querySelectorAll('[data-copy-value]'));
    if (!buttons.length) {
      return () => {};
    }

    const timers = new Map();

    const copyText = async (value) => {
      if (!value) {
        return;
      }
      if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
        await navigator.clipboard.writeText(value);
        return;
      }
      const textarea = document.createElement('textarea');
      textarea.value = value;
      textarea.setAttribute('readonly', '');
      textarea.style.position = 'absolute';
      textarea.style.left = '-9999px';
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
    };

    const handleCopy = async (event) => {
      event.preventDefault();
      event.stopPropagation();
      const button = event.currentTarget;
      const value = button.getAttribute('data-copy-value');
      try {
        await copyText(value);
        button.setAttribute('data-copied', 'true');
        if (timers.has(button)) {
          window.clearTimeout(timers.get(button));
        }
        const timeoutId = window.setTimeout(() => {
          button.removeAttribute('data-copied');
          timers.delete(button);
        }, 2000);
        timers.set(button, timeoutId);
      } catch (err) {
        console.warn('Failed to copy value', err);
      }
    };

    buttons.forEach((button) => button.addEventListener('click', handleCopy));

    return () => {
      buttons.forEach((button) => {
        button.removeEventListener('click', handleCopy);
        button.removeAttribute('data-copied');
      });
      timers.forEach((timeoutId) => window.clearTimeout(timeoutId));
      timers.clear();
    };
  };

  const module = {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }

      const hxButtons = Array.from(card.querySelectorAll('[hx-post]'));
      const refreshAction = () => {
        window.SDSM.cards.refresh('dashboard-manager');
      };
      hxButtons.forEach((btn) => {
        btn.addEventListener('click', refreshAction);
      });

      const unbindNavigation = bindCardNavigation(card);
      const unbindCopy = bindCopyButtons(card);

      return () => {
        hxButtons.forEach((btn) => {
          btn.removeEventListener('click', refreshAction);
        });
        unbindNavigation();
        unbindCopy();
      };
    }
  };

  window.SDSM.cards.define('dashboard-manager', module);
})(window);
