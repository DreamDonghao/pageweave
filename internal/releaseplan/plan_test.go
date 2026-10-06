package releaseplan

import (
	"strings"
	"testing"
)

func TestTags(t *testing.T) {
	for _, tt := range []struct {
		name, version string
		pre           bool
		releases      []Release
		want          string
	}{
		{"stable", "0.1.0", false, nil, "0.1.0,sha-0123456789ab,0.1,latest"},
		{"beta", "0.2.0-beta.1", false, nil, "0.2.0-beta.1,sha-0123456789ab"},
		{"flag", "0.2.0", true, nil, "0.2.0,sha-0123456789ab"},
		{"old patch", "0.1.0", false, []Release{{TagName: "v0.1.1"}}, "0.1.0,sha-0123456789ab"},
		{"old series", "0.1.0", false, []Release{{TagName: "v0.2.0"}}, "0.1.0,sha-0123456789ab,0.1"},
		{"higher beta", "0.1.0", false, []Release{{TagName: "v0.2.0-beta.1", Prerelease: true}}, "0.1.0,sha-0123456789ab,0.1,latest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tags, e := Tags("v"+tt.version, tt.version, "0123456789abcdef", tt.pre, tt.releases)
			if e != nil || strings.Join(tags, ",") != tt.want {
				t.Fatal(tags, e)
			}
		})
	}
}
func TestInvalid(t *testing.T) {
	for _, v := range []string{"0.01.0", "0.1", "0.1.0-beta.01", "0.1.0+metadata", "junk"} {
		if _, e := Parse(v); e == nil {
			t.Fatal(v)
		}
	}
	if _, e := Tags("v0.2.0", "0.1.0", "0123456789abcdef", false, nil); e == nil {
		t.Fatal("mismatch accepted")
	}
}
