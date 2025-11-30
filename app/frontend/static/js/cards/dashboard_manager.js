(function(window) {
  if (!window.SDSM || !window.SDSM.cards) {
    console.warn('SDSM cards subsystem missing; dashboard manager card module aborting.');
    return;
  }

  const module = {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }
      const refreshAction = () => {
        window.SDSM.cards.refresh('dashboard-manager');
      };
      card.querySelectorAll('[hx-post]').forEach((btn) => {
        btn.addEventListener('click', refreshAction);
      });
      return () => {
        card.querySelectorAll('[hx-post]').forEach((btn) => {
          btn.removeEventListener('click', refreshAction);
        });
      };
    }
  };

  window.SDSM.cards.define('dashboard-manager', module);
})(window);
