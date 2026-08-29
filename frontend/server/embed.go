// Package serverui embeds the production Server Web UI.
package serverui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var assets embed.FS

func Handler() (http.Handler, error) {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(dist)), nil
}
