package console

import "embed"

//go:embed all:dist
var embeddedStatic embed.FS
