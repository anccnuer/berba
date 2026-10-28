package templates

import (
	"embed"
	"io/fs"
)

//go:embed all:init
var initFS embed.FS

func Init() fs.FS {
	sub, err := fs.Sub(initFS, "init")
	if err != nil {
		panic(err)
	}
	return sub
}
