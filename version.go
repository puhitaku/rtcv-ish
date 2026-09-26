// Package rtcvish holds repository-level metadata shared by the core.
package rtcvish

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var release string

// Release is the release tag in the VERSION file, e.g. "v1.0.0-rc1".
var Release = strings.TrimSpace(release)
