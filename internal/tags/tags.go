// Package tags defines the store/ tag namespace that every provider shares.
//
// Any item carrying a tag under store/ is discoverable. Tags under
// store/meta/ are written automatically by store and are reserved.
package tags

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	Root       = "store"
	MetaRoot   = "store/meta"
	Managed    = "store/meta/managed/v1"
	TypeRoot   = "store/meta/type"
	HostRoot   = "store/meta/host"
	OSRoot     = "store/meta/os"
	SourceRoot = "store/meta/source"
)

// Content types recorded under TypeRoot.
const (
	TypeFile   = "file"
	TypeFolder = "folder"
	TypeText   = "text"
)

// Normalize converts a short tag (project/api) or a fully qualified tag
// (store/project/api) into its fully qualified form.
func Normalize(raw string) (string, error) {
	t := strings.Trim(strings.TrimSpace(raw), "/")
	if t == "" {
		return "", errors.New("tag cannot be empty")
	}
	if strings.Contains(t, ",") {
		return "", fmt.Errorf("tag %q cannot contain a comma", raw)
	}
	segs := strings.Split(t, "/")
	for i, s := range segs {
		s = strings.TrimSpace(s)
		if s == "" {
			return "", fmt.Errorf("tag %q contains an empty segment", raw)
		}
		segs[i] = s
	}
	t = strings.Join(segs, "/")
	if !IsStore(t) {
		t = Root + "/" + t
	}
	return t, nil
}

// NormalizeUser normalizes a label supplied by the user for an item. Labels
// under store/meta/ are rejected because store maintains them.
func NormalizeUser(raw string) (string, error) {
	t, err := Normalize(raw)
	if err != nil {
		return "", err
	}
	if t == Root {
		return "", fmt.Errorf("tag %q is too broad; use a label such as project/api", raw)
	}
	if IsMeta(t) {
		return "", fmt.Errorf("tag %q is reserved: tags under %s/ are set automatically", raw, MetaRoot)
	}
	return t, nil
}

// NormalizeAll normalizes every tag, deduplicating the result. When user is
// true, reserved metadata tags are rejected.
func NormalizeAll(raw []string, user bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, r := range raw {
		norm := Normalize
		if user {
			norm = NormalizeUser
		}
		t, err := norm(r)
		if err != nil {
			return nil, err
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

func IsStore(t string) bool { return t == Root || strings.HasPrefix(t, Root+"/") }

func IsMeta(t string) bool { return t == MetaRoot || strings.HasPrefix(t, MetaRoot+"/") }

// Discoverable reports whether an item with these tags belongs to store.
func Discoverable(ts []string) bool {
	for _, t := range ts {
		if IsStore(t) {
			return true
		}
	}
	return false
}

// Matches reports whether tag equals filter or is nested beneath it. Matching
// is by whole segment: store/project matches store/project/api but not
// store/projectile.
func Matches(tag, filter string) bool {
	return tag == filter || strings.HasPrefix(tag, filter+"/")
}

// MatchesAll reports whether every filter matches at least one tag.
func MatchesAll(ts, filters []string) bool {
	for _, f := range filters {
		found := false
		for _, t := range ts {
			if Matches(t, f) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func Has(ts []string, t string) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// Short strips the store/ prefix for display.
func Short(t string) string {
	if t == Root {
		return t
	}
	return strings.TrimPrefix(t, Root+"/")
}

// TypeOf returns the content type recorded in the tags, or "".
func TypeOf(ts []string) string {
	for _, t := range ts {
		if rest, ok := strings.CutPrefix(t, TypeRoot+"/"); ok {
			seg, _, _ := strings.Cut(rest, "/")
			return seg
		}
	}
	return ""
}

// MetaValue returns the value of the first store/meta/<key>/<value> tag, or
// "" if there is none. The value may itself contain slashes.
func MetaValue(ts []string, key string) string {
	for _, t := range ts {
		if v, ok := strings.CutPrefix(t, MetaRoot+"/"+key+"/"); ok {
			return v
		}
	}
	return ""
}

// Build joins sanitized segments beneath root. Empty segments are dropped;
// if none remain, Build returns "".
func Build(root string, segments ...string) string {
	var parts []string
	for _, s := range segments {
		s = strings.TrimSpace(s)
		s = strings.NewReplacer(",", "_", "/", "_").Replace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return root + "/" + strings.Join(parts, "/")
}

// Merge keeps every existing tag except automatic metadata, then adds the
// given groups. The result is sorted and deduplicated.
func Merge(existing []string, add ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	push := func(t string) {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range existing {
		if !IsMeta(t) {
			push(t)
		}
	}
	for _, group := range add {
		for _, t := range group {
			push(t)
		}
	}
	sort.Strings(out)
	return out
}
