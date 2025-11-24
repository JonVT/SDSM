document.addEventListener('DOMContentLoaded', function() {
    const setupForm = document.getElementById('setup-form');
    if (setupForm) {
        setupForm.addEventListener('submit', function(event) {
            const password = document.getElementById('password').value;
            const confirm = document.getElementById('confirm').value;
            const errorBox = document.getElementById('error-box');

            // Clear previous errors
            if (errorBox) {
                errorBox.remove();
            }

            if (password.length < 8) {
                event.preventDefault();
                showError('Password must be at least 8 characters long.');
                return;
            }

            if (password !== confirm) {
                event.preventDefault();
                showError('Passwords do not match.');
                return;
            }
        });
    }

    // If the server rendered an error, ensure it's visible.
    const initialError = document.getElementById('error-box');
    if (initialError && initialError.textContent.trim() === '') {
        initialError.classList.add('hidden');
    }
});

function showError(message) {
    let errorBox = document.getElementById('error-box');
    if (!errorBox) {
        errorBox = document.createElement('div');
        errorBox.id = 'error-box';
        errorBox.className = 'alert alert-danger';
        errorBox.setAttribute('role', 'alert');
        const form = document.getElementById('setup-form');
        // Insert before the first form group for better layout
        form.insertBefore(errorBox, form.firstChild);
    }
    errorBox.textContent = message;
    errorBox.classList.remove('hidden');
}
