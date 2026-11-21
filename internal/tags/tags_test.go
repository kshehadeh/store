package tags

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"project/api":       "store/project/api",
		"store/project/api": "store/project/api",
		" /project/ api/ ":  "store/project/api",
		"meta/type/file":    "store/meta/type/file",
		"store":             "store",
		"storefront":        "store/storefront",
	}
	for in, want := range cases {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "/", "a,b", "a//b"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) succeeded; want error", bad)
		}
	}
}

func TestNormalizeUserRejectsReserved(t *testing.T) {
	for _, bad := range []string{"meta/type/file", "store/meta", "store"} {
		if _, err := NormalizeUser(bad); err == nil {
			t.Errorf("NormalizeUser(%q) succeeded; want error", bad)
		}
	}
}

func TestMatchesWholeSegments(t *testing.T) {
	if !Matches("store/project/api", "store/project") {
		t.Error("store/project should match store/project/api")
	}
	if Matches("store/projectile", "store/project") {
		t.Error("store/project must not match store/projectile")
	}
	ts := []string{"store/project/api", "store/meta/type/file"}
	if !MatchesAll(ts, []string{"store/project", "store/meta/type/file"}) {
		t.Error("expected all filters to match")
	}
	if MatchesAll(ts, []string{"store/project", "store/meta/type/folder"}) {
		t.Error("expected intersection to fail")
	}
}

func TestMergeReplacesMetaKeepsLabels(t *testing.T) {
	existing := []string{"personal", "store/team/web", "store/meta/host/laptop", "store/meta/type/file"}
	got := Merge(existing, []string{"store/project/api"}, []string{"store/meta/host/desktop", "store/meta/type/text"})
	want := []string{"personal", "store/meta/host/desktop", "store/meta/type/text", "store/project/api", "store/team/web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merge = %v; want %v", got, want)
	}
}

func TestMetaValue(t *testing.T) {
	ts := []string{"store/project/api", "store/meta/host/laptop", "store/meta/source/Users/me/dev"}
	if got := MetaValue(ts, "host"); got != "laptop" {
		t.Errorf("host = %q", got)
	}
	if got := MetaValue(ts, "source"); got != "Users/me/dev" {
		t.Errorf("source = %q", got)
	}
	if got := MetaValue(ts, "os"); got != "" {
		t.Errorf("os = %q; want empty", got)
	}
}

func TestBuildSanitizes(t *testing.T) {
	if got := Build(SourceRoot, "", "Users", "me", "a,b", ""); got != "store/meta/source/Users/me/a_b" {
		t.Errorf("Build = %q", got)
	}
	if got := Build(HostRoot, " "); got != "" {
		t.Errorf("Build with no segments = %q; want empty", got)
	}
}
