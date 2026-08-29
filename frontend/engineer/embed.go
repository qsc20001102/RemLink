// Package engineerui embeds the production Wails frontend.
package engineerui

import "embed"

//go:embed all:dist
var Assets embed.FS
