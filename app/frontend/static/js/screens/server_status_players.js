(function(window) {
    'use strict';

    if (window.SDSMServerStatusPlayers) {
        return;
    }

    function toTimestamp(value) {
        if (!value) {
            return 0;
        }
        const date = new Date(value);
        return Number.isNaN(date.getTime()) ? 0 : date.getTime();
    }

    function getHistorySessionTimestamp(entry) {
        if (!entry) {
            return 0;
        }
        return toTimestamp(entry.disconnectAt) || toTimestamp(entry.connectedAt);
    }

    function sortHistorySessions(sessions) {
        return [...sessions].sort((a, b) => getHistorySessionTimestamp(b) - getHistorySessionTimestamp(a));
    }

    function formatPlayerTimestamp(value) {
        if (!value) {
            return '—';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '—';
        }
        return date.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: 'numeric',
            minute: '2-digit'
        });
    }

    function formatSessionDurationLabel(startISO, formatDurationFromStart, fallback = '0m') {
        if (!startISO) {
            return fallback;
        }
        if (typeof formatDurationFromStart !== 'function') {
            return fallback;
        }
        const label = formatDurationFromStart(startISO);
        return label || fallback;
    }

    function normalizeLivePlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                name: entry.Name || entry.name || entry.Player || 'Unknown Player',
                steamId: entry.SteamID || entry.steam_id || entry.guid || entry.GUID || '',
                isAdmin: !!(entry.IsAdmin ?? entry.is_admin),
                connectedAt: entry.ConnectDatetime || entry.connected_at || entry.connectedAt || entry.connected || '',
            };
        }).filter(Boolean);
    }

    function normalizeHistoryPlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                name: entry.Name || entry.name || entry.Player || 'Unknown Player',
                steamId: entry.SteamID || entry.steam_id || entry.guid || entry.GUID || '',
                isAdmin: !!(entry.IsAdmin ?? entry.is_admin),
                connectedAt: entry.ConnectDatetime || entry.connected_at || entry.connectedAt || entry.connected || '',
                disconnectAt: entry.DisconnectDatetime || entry.disconnect_at || entry.disconnectAt || '',
                sessionLength: entry.SessionDurationString || entry.session_length || entry.session || '',
            };
        }).filter(Boolean);
    }

    function normalizeBannedPlayers(list) {
        if (!Array.isArray(list)) {
            return [];
        }
        return list.map((entry) => {
            if (!entry) {
                return null;
            }
            return {
                steamId: entry.SteamID || entry.steam_id || entry.guid || '',
                name: entry.Name || entry.name || '',
            };
        }).filter(Boolean);
    }

    function groupHistoryEntriesByPlayer(entries) {
        const map = new Map();
        entries.forEach((entry) => {
            if (!entry) {
                return;
            }
            const normalizedKey = entry.steamId || `name:${entry.name || 'unknown'}`;
            if (!map.has(normalizedKey)) {
                map.set(normalizedKey, {
                    key: normalizedKey,
                    steamId: entry.steamId || '',
                    name: entry.name || 'Unknown Player',
                    isAdmin: !!entry.isAdmin,
                    sessions: [],
                });
            }
            const group = map.get(normalizedKey);
            group.sessions.push(entry);
            if (entry.isAdmin) {
                group.isAdmin = true;
            }
        });
        const groups = Array.from(map.values());
        groups.forEach((group) => {
            group.sessions = sortHistorySessions(group.sessions);
            group.latestStart = group.sessions[0]?.connectedAt || '';
            group.oldestStart = group.sessions[group.sessions.length - 1]?.connectedAt || '';
        });
        groups.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
        return groups;
    }

    window.SDSMServerStatusPlayers = {
        toTimestamp,
        getHistorySessionTimestamp,
        sortHistorySessions,
        formatPlayerTimestamp,
        formatSessionDurationLabel,
        normalizeLivePlayers,
        normalizeHistoryPlayers,
        normalizeBannedPlayers,
        groupHistoryEntriesByPlayer,
    };
})(window);
