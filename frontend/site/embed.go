// Package siteui embeds the Site desktop frontend.
package siteui

import "embed"

//go:embed all:dist
var Assets embed.FS
