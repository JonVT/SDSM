(function(window) {
  const SDSM = window.SDSM;
  if (!SDSM || !SDSM.cards) {
    console.warn('SDSM.cards unavailable; dashboard system health module aborted.');
    return;
  }

  const module = {
    mount(card) {
      if (!(card instanceof Element)) {
        return null;
      }
      const render = (stats) => {
        if (!SDSM.ui || typeof SDSM.ui.renderSystemHealthCard !== 'function') {
          return;
        }
        SDSM.ui.renderSystemHealthCard(stats || {}, { card });
      };

      if (SDSM.state && SDSM.state.lastStats) {
        render(SDSM.state.lastStats);
      }

      const handleStatsUpdate = (event) => {
        const stats = event?.detail || {};
        render(stats);
      };
      document.addEventListener('sdsm:stats-update', handleStatsUpdate);

      const refreshButton = card.querySelector('[data-card-refresh]');
      const handleRefresh = () => {
        SDSM.cards.refresh('dashboard-system-health');
      };
      if (refreshButton) {
        refreshButton.addEventListener('click', handleRefresh);
      }

      return () => {
        document.removeEventListener('sdsm:stats-update', handleStatsUpdate);
        if (refreshButton) {
          refreshButton.removeEventListener('click', handleRefresh);
        }
      };
    }
  };

  SDSM.cards.define('dashboard-system-health', module);
})(window);
