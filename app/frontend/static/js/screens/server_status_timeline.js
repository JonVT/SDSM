(function(window, document) {
    'use strict';

    if (window.SDSMPlayerTimeline) {
        return;
    }
    window.SDSMPlayerTimeline = true;

    const CARD_ID = 'server-status-players';
    const PRESETS = [
        { label: 'Auto', hours: 0 },
        { label: '6h', hours: 6 },
        { label: '12h', hours: 12 },
        { label: '24h', hours: 24 },
        { label: '48h', hours: 48 },
        { label: '72h', hours: 72 },
        { label: '7d', hours: 168 },
        { label: '14d', hours: 336 }
    ];
    const HOUR = 3600;

    function serverKey() {
        const card = document.getElementById('players-card');
        const url = card ? card.getAttribute('hx-get') || '' : '';
        return 'sdsm.timeline.range.' + url;
    }

    function loadRange() {
        try {
            return JSON.parse(window.localStorage.getItem(serverKey()) || 'null');
        } catch (e) {
            return null;
        }
    }

    function saveRange(range) {
        try {
            if (range) {
                window.localStorage.setItem(serverKey(), JSON.stringify(range));
            } else {
                window.localStorage.removeItem(serverKey());
            }
        } catch (e) { /* storage unavailable */ }
    }

    // Resolve a stored selection to concrete unix seconds; presets slide with the clock.
    function resolveRange(range) {
        if (!range) {
            return null;
        }
        if (range.mode === 'preset' && range.hours > 0) {
            const end = (Math.floor(Date.now() / 1000 / HOUR) + 1) * HOUR;
            return { start: end - range.hours * HOUR, end: end };
        }
        if (range.mode === 'custom' && range.start > 0 && range.end > range.start) {
            return { start: range.start, end: range.end };
        }
        return null;
    }

    function toLocalInput(unix) {
        const d = new Date(unix * 1000);
        const pad = (n) => String(n).padStart(2, '0');
        return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + 'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
    }

    function cardURL() {
        const card = document.getElementById('players-card');
        return card ? card.getAttribute('hx-get') : null;
    }

    function withRange(url, range) {
        const resolved = resolveRange(range);
        if (!resolved) {
            return url;
        }
        const sep = url.indexOf('?') >= 0 ? '&' : '?';
        return url + sep + 'tl_start=' + resolved.start + '&tl_end=' + resolved.end;
    }

    // Keep the chosen range when the whole card refreshes.
    document.body.addEventListener('htmx:configRequest', (event) => {
        const path = event.detail && event.detail.path;
        const base = cardURL();
        if (!path || !base || path.split('?')[0] !== base.split('?')[0]) {
            return;
        }
        const resolved = resolveRange(loadRange());
        if (resolved) {
            event.detail.parameters.tl_start = resolved.start;
            event.detail.parameters.tl_end = resolved.end;
        }
    });

    async function reloadTimeline() {
        const base = cardURL();
        const panel = document.getElementById('players-timeline-panel');
        if (!base || !panel) {
            return;
        }
        const response = await fetch(withRange(base, loadRange()), { credentials: 'same-origin', headers: { 'HX-Request': 'true' } });
        if (!response.ok) {
            throw new Error('Timeline request failed (' + response.status + ')');
        }
        const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
        const fresh = doc.getElementById('players-timeline-panel');
        if (!fresh) {
            return;
        }
        panel.innerHTML = fresh.innerHTML;
        if (window.feather && typeof window.feather.replace === 'function') {
            window.feather.replace();
        }
    }

    function closePopover() {
        document.querySelectorAll('.timeline-range-popover').forEach((el) => el.remove());
        document.querySelectorAll('[data-timeline-range-toggle]').forEach((el) => el.setAttribute('aria-expanded', 'false'));
    }

    function apply(range) {
        saveRange(range);
        closePopover();
        reloadTimeline().catch((err) => {
            if (window.console) {
                console.error(err);
            }
        });
    }

    function openPopover(timeline) {
        closePopover();
        const stored = loadRange();
        const startUnix = parseInt(timeline.dataset.timelineStart, 10) || 0;
        const endUnix = parseInt(timeline.dataset.timelineEnd, 10) || 0;

        const pop = document.createElement('div');
        pop.className = 'timeline-range-popover';
        pop.setAttribute('role', 'dialog');
        pop.setAttribute('aria-label', 'Timeline range');

        const presets = document.createElement('div');
        presets.className = 'timeline-range-presets';
        PRESETS.forEach((p) => {
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'btn btn-sm btn-ghost';
            btn.textContent = p.label;
            const active = (!stored && p.hours === 0) || (stored && stored.mode === 'preset' && stored.hours === p.hours);
            if (active) {
                btn.classList.add('is-active');
            }
            btn.addEventListener('click', () => apply(p.hours ? { mode: 'preset', hours: p.hours } : null));
            presets.appendChild(btn);
        });
        pop.appendChild(presets);

        const form = document.createElement('form');
        form.className = 'timeline-range-custom';
        form.innerHTML =
            '<label>From <input type="datetime-local" name="start" class="form-control" required></label>' +
            '<label>To <input type="datetime-local" name="end" class="form-control" required></label>' +
            '<div class="timeline-range-error" role="alert" hidden></div>' +
            '<button type="submit" class="btn btn-sm btn-primary">Apply</button>';
        form.elements.start.value = toLocalInput(startUnix);
        form.elements.end.value = toLocalInput(endUnix);
        form.addEventListener('submit', (event) => {
            event.preventDefault();
            const start = Math.floor(new Date(form.elements.start.value).getTime() / 1000);
            const end = Math.floor(new Date(form.elements.end.value).getTime() / 1000);
            const error = form.querySelector('.timeline-range-error');
            if (!(start > 0) || !(end > start)) {
                error.textContent = 'End must be after start.';
                error.hidden = false;
                return;
            }
            apply({ mode: 'custom', start: start, end: end });
        });
        pop.appendChild(form);

        timeline.appendChild(pop);
        document.querySelectorAll('[data-timeline-range-toggle]').forEach((el) => el.setAttribute('aria-expanded', 'true'));
    }

    document.addEventListener('click', (event) => {
        const toggle = event.target.closest('[data-timeline-range-toggle]');
        if (toggle) {
            const timeline = toggle.closest('.player-timeline');
            if (!timeline) {
                return;
            }
            if (timeline.querySelector('.timeline-range-popover')) {
                closePopover();
            } else {
                openPopover(timeline);
            }
            return;
        }
        if (!event.target.closest('.timeline-range-popover')) {
            closePopover();
        }
    });

    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') {
            closePopover();
            return;
        }
        if ((event.key === 'Enter' || event.key === ' ') && event.target.matches && event.target.matches('.timeline-axis[data-timeline-range-toggle]')) {
            event.preventDefault();
            event.target.click();
        }
    });

    // Initial page load: apply a remembered range once the card exists.
    document.addEventListener('DOMContentLoaded', () => {
        if (loadRange()) {
            reloadTimeline().catch(() => {});
        }
    });
})(window, document);
