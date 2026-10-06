package pomogayka

import "embed"

//go:embed all:web/templates all:web/static all:migrations
var Content embed.FS
