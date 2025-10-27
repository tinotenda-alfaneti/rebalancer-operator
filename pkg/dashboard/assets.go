package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var static embed.FS

func staticFS() (fs.FS, error) {
	return fs.Sub(static, "static")
}
