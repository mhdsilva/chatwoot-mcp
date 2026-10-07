// Package web provides the embedded assets for the local setup panel.
package web

import "embed"

// Assets contains the panel files. index.html is an html/template template and
// expects a CSRFToken field in its data when rendered by the panel server.
//
//go:embed index.html app.js style.css
var Assets embed.FS
