(function(window, document) {
    'use strict';

    if (window.SDSMServerStatusUtils) {
        return;
    }

    function parseCSVList(value) {
        if (!value) {
            return [];
        }
        return value
            .split(',')
            .map((entry) => entry.trim())
            .filter((entry) => entry.length > 0);
    }

    function readJSONScript(id) {
        const el = document.getElementById(id);
        if (!el) {
            return null;
        }
        const text = (el.textContent || '').trim();
        if (!text) {
            return null;
        }
        try {
            return JSON.parse(text);
        } catch (error) {
            console.warn(`Unable to parse JSON from #${id}`, error);
            return null;
        }
    }

    function buildQuery(params = {}) {
        const search = new URLSearchParams();
        Object.entries(params).forEach(([key, value]) => {
            if (value !== undefined && value !== null && value !== '') {
                search.append(key, value);
            }
        });
        const query = search.toString();
        return query ? `?${query}` : '';
    }

    function formatDateTime(value) {
        if (!value) {
            return '—';
        }
        const date = value instanceof Date ? value : new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '—';
        }
        return date.toLocaleString();
    }

    function formatDurationFromStart(startISO) {
        if (!startISO) {
            return '0m';
        }
        const start = new Date(startISO);
        if (Number.isNaN(start.getTime())) {
            return '0m';
        }
        let diff = Date.now() - start.getTime();
        if (diff <= 0) {
            return '0m';
        }
        const mins = Math.floor(diff / 60000);
        if (mins < 1) {
            return `${Math.max(1, Math.floor(diff / 1000))}s`;
        }
        const hours = Math.floor(mins / 60);
        const days = Math.floor(hours / 24);
        if (days > 0) {
            const remHours = hours % 24;
            return `${days}d ${String(remHours).padStart(2, '0')}h`;
        }
        if (hours > 0) {
            const remMins = mins % 60;
            return `${hours}h ${String(remMins).padStart(2, '0')}m`;
        }
        return `${mins}m`;
    }

    window.SDSMServerStatusUtils = {
        parseCSVList,
        readJSONScript,
        buildQuery,
        formatDateTime,
        formatDurationFromStart
    };
})(window, document);
