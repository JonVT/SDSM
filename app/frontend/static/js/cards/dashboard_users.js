(function(window) {
  if (!window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard users card module aborting.');
    return;
  }

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
      return () => {
        if (refreshButton) {
          refreshButton.removeEventListener('click', refreshAction);
        }
      };
    }
  };

  window.SDSM.cards.define('dashboard-users', module);
})(window);
