// Package provider defines the Provider interface and the registry that
// merges sessions across providers. Adding a provider (e.g. Gemini) means
// one new package plus one entry in the registry slice.
package provider

import (
	"fmt"
	"sort"

	"github.com/QuietSugar/resumer/internal/session"
)

// Provider is the contract each AI-CLI session source implements.
// Implementations must read their env overrides at call time (not init)
// so test harnesses can inject fixture roots per invocation.
type Provider interface {
	Name() string
	Badge() string
	BadgeANSI() string
	IsAvailable() bool
	ListSessions(f session.Filters) ([]session.Session, error)
	LoadDetail(id string) (*session.Session, error)
}

var registry []Provider

// disabled holds provider names turned off by the user (persisted in the
// config file, applied via SetDisabled). A disabled provider is skipped by
// every ambient path — Active() never yields it, so nothing walks or parses
// its storage. Explicit per-invocation requests (--source NAME) bypass the
// flag on purpose: the config supplies defaults, the command line overrides.
var disabled = map[string]bool{}

// SetDisabled replaces the disabled set from the config file. Unknown names
// are kept: the config outlives registry changes.
func SetDisabled(names []string) {
	disabled = make(map[string]bool, len(names))
	for _, n := range names {
		disabled[n] = true
	}
}

// IsEnabled reports whether name is not in the disabled set.
func IsEnabled(name string) bool { return !disabled[name] }

// DisabledNames returns the disabled provider names, sorted.
func DisabledNames() []string {
	out := make([]string, 0, len(disabled))
	for n := range disabled {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Register appends a provider. Called from cli wiring to avoid import cycles
// and keep this package dependency-free for tests.
func Register(p Provider) {
	registry = append(registry, p)
}

// All returns every registered provider, disabled or not.
func All() []Provider {
	out := make([]Provider, len(registry))
	copy(out, registry)
	return out
}

// Active returns providers whose data sources are present on this machine
// and not disabled by the user.
func Active() []Provider {
	var out []Provider
	for _, p := range registry {
		if !IsEnabled(p.Name()) {
			continue
		}
		if p.IsAvailable() {
			out = append(out, p)
		}
	}
	return out
}

// Get returns the provider with the given name, or nil.
func Get(name string) Provider {
	for _, p := range registry {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// AvailableSourceNames lists names of active providers.
func AvailableSourceNames() []string {
	var out []string
	for _, p := range Active() {
		out = append(out, p.Name())
	}
	return out
}

// SortSessions orders by (LastTS, Source) descending — string comparison,
// matching the Python registry's sort for output parity — with Path as a
// final tiebreak for deterministic ordering on identical timestamps.
// Equal elements must compare false in both directions (strict weak
// ordering), or sort.SliceStable's behavior is undefined.
func SortSessions(out []session.Session, ascending bool) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.LastTS != b.LastTS {
			if ascending {
				return a.LastTS < b.LastTS
			}
			return a.LastTS > b.LastTS
		}
		if a.Source != b.Source {
			if ascending {
				return a.Source < b.Source
			}
			return a.Source > b.Source
		}
		if a.Path != b.Path {
			if ascending {
				return a.Path > b.Path
			}
			return a.Path < b.Path
		}
		return false
	})
}

// MergedList collects sessions from active providers (or the single provider
// named in filters), sorted last-activity-descending, limit applied.
func MergedList(f session.Filters) ([]session.Session, error) {
	var out []session.Session
	if f.Source != "" {
		p := Get(f.Source)
		if p == nil {
			return nil, fmt.Errorf("unknown provider: %s", f.Source)
		}
		if !p.IsAvailable() {
			return nil, fmt.Errorf(
				"%s provider not available (binary or session directory missing)", f.Source)
		}
		ss, err := p.ListSessions(f)
		if err != nil {
			return nil, err
		}
		out = append(out, ss...)
	} else {
		for _, p := range Active() {
			ss, err := p.ListSessions(f)
			if err != nil {
				return nil, err
			}
			out = append(out, ss...)
		}
	}
	SortSessions(out, false)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
