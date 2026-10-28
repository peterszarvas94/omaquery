// Package omaquery contains the Omarchy plugin files shipped with the CLI.
package omaquery

import "embed"

// PluginFS holds the plugin manifest and QML entry point. Keeping the files at
// the repository root lets Omarchy's plugin manager install them directly,
// while embedding them lets the CLI install or update the plugin without a
// checkout or network access on the target machine.
//
//go:embed manifest.json Popup.qml
var PluginFS embed.FS
