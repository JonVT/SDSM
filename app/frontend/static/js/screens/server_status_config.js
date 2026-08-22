(function(window) {
    'use strict';

    if (window.SDSMServerStatusConfig) {
        return;
    }

    function getVersionKeyFromSelect(select) {
        if (!select) {
            return 'release';
        }
        return select.value === 'true' ? 'beta' : 'release';
    }

    function populateSelectOptions(select, items, preferredValue, options = {}) {
        const { valueKey = 'value', labelKey = 'label', emptyLabel = 'Select an option' } = options;
        if (!select) {
            return;
        }
        select.innerHTML = '';
        if (!Array.isArray(items) || !items.length) {
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = emptyLabel;
            select.appendChild(placeholder);
            select.value = '';
            select.disabled = true;
            select.dataset.currentValue = '';
            return;
        }
        select.disabled = false;
        items.forEach((item) => {
            if (!item) {
                return;
            }
            const option = document.createElement('option');
            const normalizedValueKey = typeof valueKey === 'string' ? valueKey : 'value';
            const normalizedLabelKey = typeof labelKey === 'string' ? labelKey : 'label';
            const lowerValueKey = normalizedValueKey.toLowerCase();
            const lowerLabelKey = normalizedLabelKey.toLowerCase();
            const value = item[normalizedValueKey] ?? item[lowerValueKey] ?? item.id ?? item.ID ?? '';
            const label = item[normalizedLabelKey] ?? item[lowerLabelKey] ?? (value || 'Option');
            option.value = value;
            option.textContent = label;
            if (item.Description || item.description) {
                option.title = item.Description || item.description;
            }
            select.appendChild(option);
        });
        let nextValue = preferredValue;
        if (!nextValue || !Array.from(select.options).some((opt) => opt.value === nextValue)) {
            nextValue = select.dataset.currentValue || '';
        }
        if (!nextValue || !Array.from(select.options).some((opt) => opt.value === nextValue)) {
            nextValue = select.options[0]?.value || '';
        }
        select.value = nextValue;
        select.dataset.currentValue = select.value || '';
    }

    window.SDSMServerStatusConfig = {
        getVersionKeyFromSelect,
        populateSelectOptions,
    };
})(window);
