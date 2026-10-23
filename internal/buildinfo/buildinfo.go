// SPDX-License-Identifier: AGPL-3.0-or-later

// Package buildinfo reports the version of the running flowi binary.
package buildinfo

import "runtime/debug"

const (
	devVersion        = "dev"
	develMarker       = "(devel)"
	shortRevisionSize = 12
)

// Version returns the module version for tagged builds, "dev+<revision>"
// for local builds with VCS metadata, and "dev" otherwise.
func Version() string {
	return versionFrom(debug.ReadBuildInfo())
}

func versionFrom(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return devVersion
	}
	if v := info.Main.Version; v != "" && v != develMarker {
		return v
	}
	revision := settingValue(info, "vcs.revision")
	if revision == "" {
		return devVersion
	}
	return devVersion + "+" + revision[:min(len(revision), shortRevisionSize)]
}

func settingValue(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
