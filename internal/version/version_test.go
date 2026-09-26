package version

import (
	"runtime/debug"
	"testing"

	rtcvish "github.com/puhitaku/rtcv-ish"
)

func reader(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Settings: settings}, true }
}

func noBuildInfo() (*debug.BuildInfo, bool) { return nil, false }

func TestGet(t *testing.T) {
	const rev = "0123456789abcdef0123456789abcdef01234567"
	rel := rtcvish.Release
	for _, tc := range []struct {
		name   string
		read   func() (*debug.BuildInfo, bool)
		kind   string
		want   string
		commit string
		dirty  bool
	}{
		{"dev clean", reader(debug.BuildSetting{Key: "vcs.revision", Value: rev}, debug.BuildSetting{Key: "vcs.modified", Value: "false"}), "", "0123456", "0123456", false},
		{"dev dirty", reader(debug.BuildSetting{Key: "vcs.revision", Value: rev}, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "", "0123456-dirty", "0123456", true},
		{"release", reader(debug.BuildSetting{Key: "vcs.revision", Value: rev}, debug.BuildSetting{Key: "vcs.modified", Value: "false"}), "release", rel + " 0123456", "0123456", false},
		{"release dirty", reader(debug.BuildSetting{Key: "vcs.revision", Value: rev}, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "release", rel + " 0123456", "0123456", true},
		{"no vcs", reader(), "", "unknown", "", false},
		{"no vcs modified", reader(debug.BuildSetting{Key: "vcs.modified", Value: "true"}), "", "unknown", "", false},
		{"no build info", noBuildInfo, "", "unknown", "", false},
		{"release no vcs", noBuildInfo, "release", rel + " unknown", "", false},
		{"short revision", reader(debug.BuildSetting{Key: "vcs.revision", Value: "abc"}), "", "abc", "abc", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := get(tc.read, tc.kind)
			if got := info.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			if info.Commit != tc.commit || info.Dirty != tc.dirty || info.Release != rel {
				t.Errorf("info = %+v, want commit %q dirty %v release %q", info, tc.commit, tc.dirty, rel)
			}
			wantKind := KindDev
			if tc.kind == KindRelease {
				wantKind = KindRelease
			}
			if info.Kind != wantKind {
				t.Errorf("Kind = %q, want %q", info.Kind, wantKind)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	if rtcvish.Release == "" || rtcvish.Release[0] != 'v' {
		t.Errorf("Release = %q, want a v-prefixed tag", rtcvish.Release)
	}
}
