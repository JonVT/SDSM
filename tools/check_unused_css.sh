#!/usr/bin/env bash
set -euo pipefail

# Simple heuristic check for unused CSS class selectors in ui/static/ui-theme.css
# Requires: grep, sed, awk

CSS_FILE="$(dirname "$0")/../ui/static/ui-theme.css"
TEMPLATES_DIR="$(dirname "$0")/../ui/templates"

if [[ ! -f "$CSS_FILE" ]]; then
  echo "CSS file not found: $CSS_FILE" >&2
  exit 1
fi

if [[ ! -d "$TEMPLATES_DIR" ]]; then
  echo "Templates dir not found: $TEMPLATES_DIR" >&2
  exit 1
fi

# Extract simple class selectors like .class-name from CSS (ignore pseudo/classes with ':' and complex selectors with spaces)
# Extract class selectors that begin a selector token (reduces false positives from numbers/units)
classes=$(grep -oE '^[[:space:]]*\.[a-zA-Z][a-zA-Z0-9_-]*' "$CSS_FILE" | sed -E 's/^[[:space:]]*\.//' | sort -u)

unused=()
for cls in $classes; do
  # Skip very generic/common class names we know are structural or not referenced literally (e.g., btn variants used via composition)
    case "$cls" in
      btn|btn-primary|btn-secondary|btn-danger|btn-success|btn-warning|btn-control|button-group|container|card|glass-card|alert|tab|tabs|tab-btn|tab-panel|hidden|inline-flex|htmx-request|player-banned-icon|row-banned|status-idle|status-running|status-error|status-complete)
        continue;;
    esac
    if ! grep -Rqs --include='*.html' -E 'class="[^\"]*\b'"$cls"'\b' "$TEMPLATES_DIR"; then
      # Also consider dynamic class usage via JS in .html and .js files
      dyn_pattern="classList\\.(add|remove|toggle|contains|replace)\\([^)]*['\"]${cls}['\"][^)]*\\)|\\.className[[:space:]]*([+]?=)[[:space:]]*['\"][^\"']*\\b${cls}\\b"
      if ! grep -Rqs -E "$dyn_pattern" --include='*.html' --include='*.js' "$TEMPLATES_DIR"; then
        unused+=("$cls")
      fi
    fi
done

if [[ ${#unused[@]} -eq 0 ]]; then
  echo "No obviously unused CSS classes detected."
else
  echo "Possibly unused CSS classes (heuristic):"
  for c in "${unused[@]}"; do
    echo "  - $c"
  done
  exit 2
fi
