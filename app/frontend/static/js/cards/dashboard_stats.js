(function(window) {
  const SDSM = window.SDSM;
  if (!SDSM || !SDSM.cards) {
    console.warn('SDSM.cards unavailable; dashboard stats module aborted.');
    return;
  }

  const module = {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }

      const refreshButton = card.querySelector('[data-card-refresh]');
      const handleRefresh = () => {
        SDSM.cards.refresh('dashboard-stats');
      };
      if (refreshButton) {
        refreshButton.addEventListener('click', handleRefresh);
      }

      const handleStatsUpdate = (event) => {
        const stats = event?.detail || {};
        if (SDSM.ui && typeof SDSM.ui.updateStats === 'function') {
          SDSM.ui.updateStats(stats);
        }
      };
      document.addEventListener('sdsm:stats-update', handleStatsUpdate);

      if (SDSM.state && SDSM.state.lastStats && SDSM.ui && typeof SDSM.ui.updateStats === 'function') {
        SDSM.ui.updateStats(SDSM.state.lastStats);
      }

      return () => {
        document.removeEventListener('sdsm:stats-update', handleStatsUpdate);
        if (refreshButton) {
          refreshButton.removeEventListener('click', handleRefresh);
        }
      };
    }
  };

  SDSM.cards.define('dashboard-stats', module);
})(window);
