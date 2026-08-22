(function(window) {
    'use strict';

    if (window.SDSMServerStatusChat) {
        return;
    }

    function formatChatTimestamp(value) {
        if (!value) {
            return '';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return '';
        }
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    function normalizeChatMessage(entry) {
        if (!entry) {
            return null;
        }
        return {
            author: entry.author || entry.player || entry.name || entry.Author || entry.Player || 'Server',
            text: entry.text || entry.message || entry.Message || '',
            timestamp: entry.timestamp || entry.time || entry.datetime || entry.Datetime || entry.Timestamp || '',
        };
    }

    window.SDSMServerStatusChat = {
        formatChatTimestamp,
        normalizeChatMessage,
    };
})(window);
