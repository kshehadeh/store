// Package providers lists the vault providers store supports.
package providers

import (
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/provider/onepassword"
)

func All() []provider.Definition {
	return []provider.Definition{onepassword.Definition()}
}

func Lookup(name string) (provider.Definition, bool) {
	for _, d := range All() {
		if d.Name == name {
			return d, true
		}
	}
	return provider.Definition{}, false
}
