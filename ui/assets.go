package ui

import "embed"

// Assets embeds templates and static directories into the binary.
//
//go:embed templates static
var Assets embed.FS
