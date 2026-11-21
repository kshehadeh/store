package cli

import "runtime/debug"

// Version is set from the Git tag by Task and GoReleaser at build time.
var Version = "dev"

func commandVersion() string {
	info, _ := debug.ReadBuildInfo()
	return resolveVersion(Version, info)
}

func resolveVersion(version string, info *debug.BuildInfo) string {
	if version != "dev" {
		return version
	}
	// go install ...@VERSION sets the module version without our linker flags.
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
