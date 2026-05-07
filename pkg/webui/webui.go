package webui

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assetsFS embed.FS

// FS returns the shared web-asset filesystem rooted at "assets/".
func FS() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// CSS returns the canonical stylesheet bytes.
func CSS() []byte {
	b, _ := fs.ReadFile(assetsFS, "assets/style.css")
	return b
}
