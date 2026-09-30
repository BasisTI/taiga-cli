package cli

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// moduleVersion matches a module version as `go install ...@version` records it: a release,
// a pre-release or a pseudo-version, optionally with build metadata such as +dirty.
var moduleVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// resolveVersion prefers the ldflags value (GoReleaser); when it is still the "dev" default,
// it uses the main module version from the build info, without the leading v to match the
// GoReleaser form. A build in a checkout reports "(devel)" and stays "dev".
func resolveVersion(ldflags string, info *debug.BuildInfo, ok bool) string {
	if ldflags != "dev" || !ok || info == nil || !moduleVersion.MatchString(info.Main.Version) {
		return ldflags
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}

func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(Version, info, ok)
}
