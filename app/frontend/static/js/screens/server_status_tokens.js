(function(window, document) {
    'use strict';

    if (window.SDSMTokenPicker) {
        return;
    }
    window.SDSMTokenPicker = true;

    function insertAtCursor(input, text) {
        const start = typeof input.selectionStart === 'number' ? input.selectionStart : input.value.length;
        const end = typeof input.selectionEnd === 'number' ? input.selectionEnd : start;
        input.value = input.value.slice(0, start) + text + input.value.slice(end);
        const caret = start + text.length;
        input.focus();
        input.setSelectionRange(caret, caret);
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));
    }

    document.addEventListener('click', (event) => {
        const chip = event.target.closest('.token-chip');
        if (chip) {
            const picker = chip.closest('[data-token-picker]');
            const input = picker && document.getElementById(picker.dataset.tokenTarget);
            if (input) {
                insertAtCursor(input, chip.dataset.token || '');
                picker.removeAttribute('open');
            }
            return;
        }
        // Close any open picker when clicking elsewhere
        document.querySelectorAll('[data-token-picker][open]').forEach((picker) => {
            if (!picker.contains(event.target)) {
                picker.removeAttribute('open');
            }
        });
    });

    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') {
            document.querySelectorAll('[data-token-picker][open]').forEach((picker) => picker.removeAttribute('open'));
        }
    });
})(window, document);
