(function(window) {
    'use strict';

    if (window.SDSMServerStatusSaves) {
        return;
    }

    function formatSaveDate(value) {
        if (!value) {
            return 'Unknown time';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return 'Unknown time';
        }
        return date.toLocaleString();
    }

    function normalizeSaveItems(type, data, labels) {
        if (!data) return [];
        const saveTypeLabels = labels || {};
        if (type === 'player') {
            const groups = Array.isArray(data.groups) ? data.groups : [];
            const normalizedPlayers = [];
            groups.forEach(group => {
                const items = Array.isArray(group.items) ? group.items : [];
                items.forEach(item => {
                    normalizedPlayers.push({
                        type: 'player',
                        filename: item.filename,
                        label: item.filename ? item.filename.replace(/\.save$/i, '') : 'Player Save',
                        datetime: item.datetime,
                        typeLabel: saveTypeLabels.player || 'Player Save',
                        playerName: group.name || '',
                        steamId: group.steam_id || '',
                    });
                });
            });
            return normalizedPlayers;
        }

        const items = Array.isArray(data.items) ? data.items : [];
        return items.map(item => ({
            type: type === 'manual' ? 'manual' : type,
            filename: item.filename,
            label: item.name || (item.filename ? item.filename.replace(/\.save$/i, '') : 'Save'),
            datetime: item.datetime,
            typeLabel: saveTypeLabels[type] || 'Save',
        }));
    }

    function normalizePlayerSaveGroups(data) {
        const groups = Array.isArray(data?.groups) ? data.groups : [];
        return groups.map((group) => {
            const steamId = group?.steam_id || '';
            const playerName = group?.name || steamId || 'Unknown Player';
            const saves = (Array.isArray(group?.items) ? group.items : [])
                .map((item) => ({
                    type: 'player',
                    filename: item.filename,
                    label: item.name || (item.filename ? item.filename.replace(/\.save$/i, '') : 'Player Save'),
                    datetime: item.datetime,
                }))
                .sort((a, b) => {
                    const aTime = a?.datetime ? new Date(a.datetime).getTime() : 0;
                    const bTime = b?.datetime ? new Date(b.datetime).getTime() : 0;
                    return bTime - aTime;
                });
            const latestTime = saves.length ? (saves[0].datetime || '') : '';
            return {
                steamId,
                playerName,
                saves,
                latestTime,
            };
        }).sort((a, b) => {
            const aTime = a?.latestTime ? new Date(a.latestTime).getTime() : 0;
            const bTime = b?.latestTime ? new Date(b.latestTime).getTime() : 0;
            if (bTime !== aTime) {
                return bTime - aTime;
            }
            return (a.playerName || '').localeCompare(b.playerName || '');
        });
    }

    window.SDSMServerStatusSaves = {
        formatSaveDate,
        normalizeSaveItems,
        normalizePlayerSaveGroups,
    };
})(window);
