// Package frontend embeds the static page into the server binary.
package frontend

import "embed"

//go:embed index.html styles.css app.js js
var Files embed.FS
