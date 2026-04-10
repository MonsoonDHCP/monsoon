package dashboard

import (
	"embed"
	"io/fs"
)

// Dist embeds the production dashboard bundle so Monsoon can serve the UI from a single binary.
//
//go:embed dist dist/*
var Dist embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(Dist, "dist")
}
