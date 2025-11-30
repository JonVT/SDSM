(function(window) {
  if (!window || !window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard server deck card module aborting.');
    return;
  }

  const cardId = 'dashboard-server-deck';

  function bindNavigation(card) {
    if (!window.SDSM || !SDSM.ui || typeof SDSM.ui.bindServerCardNavigation !== 'function') {
      return;
    }
    const grid = card.querySelector('#server-grid');
    if (grid) {
      SDSM.ui.bindServerCardNavigation(grid);
    }
  }

  window.SDSM.cards.define(cardId, {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }

      const refreshButton = card.querySelector('[data-card-refresh]');
      const handleRefresh = () => window.SDSM.cards.refresh(cardId);
      if (refreshButton) {
        refreshButton.addEventListener('click', handleRefresh);
      }

      const rebind = () => bindNavigation(card);
      const handleAfterSwap = (event) => {
        if (!event || !event.target) {
          return;
        }
        if (card.contains(event.target) && event.target.id === 'server-grid') {
          rebind();
        }
      };

      // Initial bind once the DOM is ready
      requestAnimationFrame(rebind);
      card.addEventListener('htmx:afterSwap', handleAfterSwap);

      return () => {
        if (refreshButton) {
          refreshButton.removeEventListener('click', handleRefresh);
        }
        card.removeEventListener('htmx:afterSwap', handleAfterSwap);
      };
    }
  });
})(window);
