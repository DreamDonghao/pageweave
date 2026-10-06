// Package releaseplan determines release aliases without rolling them backwards.
package releaseplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// Version represents release versions that are safe as exact Docker tags.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 string
	Text                string
}

// Parse validates the release SemVer form, excluding Docker-incompatible build metadata.
func Parse(raw string) (Version, error) {
	m := pattern.FindStringSubmatch(raw)
	if m == nil {
		return Version{}, fmt.Errorf("invalid release version %q", raw)
	}
	v := Version{Pre: m[4], Text: strings.TrimPrefix(raw, "v")}
	var e error
	for i, dst := range []*uint64{&v.Major, &v.Minor, &v.Patch} {
		*dst, e = strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return Version{}, e
		}
	}
	for _, part := range strings.Split(v.Pre, ".") {
		if len(part) > 1 && part[0] == '0' {
			if _, e := strconv.ParseUint(part, 10, 64); e == nil {
				return Version{}, fmt.Errorf("leading zero in prerelease")
			}
		}
	}
	return v, nil
}
func greater(a, b Version) bool {
	if a.Major != b.Major {
		return a.Major > b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor > b.Minor
	}
	return a.Patch > b.Patch
}

// Release is the minimal GitHub Release information used for alias decisions.
type Release struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// Tags returns exact and SHA tags, plus stable aliases that cannot roll backwards.
func Tags(tag, version, sha string, currentPre bool, releases []Release) ([]string, error) {
	v, e := Parse(version)
	if e != nil {
		return nil, e
	}
	if strings.HasPrefix(version, "v") || tag != "v"+version {
		return nil, fmt.Errorf("tag and VERSION differ")
	}
	if len(sha) < 12 {
		return nil, fmt.Errorf("commit SHA missing")
	}
	tags := []string{v.Text, "sha-" + sha[:12]}
	if currentPre || v.Pre != "" {
		return tags, nil
	}
	latest, series := true, true
	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		other, e := Parse(r.TagName)
		if e != nil || other.Pre != "" {
			continue
		}
		if greater(other, v) {
			latest = false
		}
		if other.Major == v.Major && other.Minor == v.Minor && greater(other, v) {
			series = false
		}
	}
	if series {
		tags = append(tags, fmt.Sprintf("%d.%d", v.Major, v.Minor))
	}
	if latest {
		tags = append(tags, "latest")
	}
	return tags, nil
}
