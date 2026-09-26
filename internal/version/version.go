// Package version composes the core version string from the VERSION file
// and the VCS information the Go toolchain stamps into the binary.
package version

import (
	"runtime/debug"

	rtcvish "github.com/puhitaku/rtcv-ish"
)

const (
	KindRelease = "release"
	KindDev     = "dev"
)

// kind is set to "release" by release builds with
// -ldflags "-X github.com/puhitaku/rtcv-ish/internal/version.kind=release".
var kind string

// Info is the version of the running binary.
type Info struct {
	// Release is the tag in the VERSION file.
	Release string
	// Commit is the abbreviated VCS revision; empty without VCS info.
	Commit string
	// Dirty is set when the working tree had uncommitted changes.
	Dirty bool
	// Kind is KindRelease for release builds, KindDev otherwise.
	Kind string
}

// String is "<release> <commit>" for release builds and "<commit>" or
// "<commit>-dirty" otherwise; "unknown" stands in for a missing commit.
func (i Info) String() string {
	commit := i.Commit
	if commit == "" {
		commit = "unknown"
	}
	if i.Kind == KindRelease {
		return i.Release + " " + commit
	}
	if i.Dirty && i.Commit != "" {
		return commit + "-dirty"
	}
	return commit
}

// Get returns the version of the running binary.
func Get() Info { return get(debug.ReadBuildInfo, kind) }

// String returns Get().String().
func String() string { return Get().String() }

func get(read func() (*debug.BuildInfo, bool), kind string) Info {
	info := Info{Release: rtcvish.Release, Kind: KindDev}
	if kind == KindRelease {
		info.Kind = KindRelease
	}
	bi, ok := read()
	if !ok {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value[:min(7, len(s.Value))]
		case "vcs.modified":
			info.Dirty = s.Value == "true"
		}
	}
	if info.Commit == "" {
		info.Dirty = false
	}
	return info
}
