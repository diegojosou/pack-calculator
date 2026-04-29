// Package web embeds the static UI assets so they ship inside the server
// binary. The //go:embed directive can only see paths relative to the file
// it lives in, which is why this lives next to the assets/ directory.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var embedded embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}
