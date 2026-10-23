// SPDX-License-Identifier: AGPL-3.0-or-later

// Package flowicli holds files that ship inside the flowi binary.
package flowicli

import _ "embed"

//go:embed CHANGELOG.md
var changelog string

// Changelog returns the text of CHANGELOG.md as it was when flowi was built.
func Changelog() string { return changelog }
