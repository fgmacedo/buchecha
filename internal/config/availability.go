package config

import (
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
)

// Availability is the diagnostic report ResolveAvailability returns
// alongside the filtered Config. The fields are non-fatal observations
// the caller may surface as warnings.
type Availability struct {
	// AvailableProviders lists provider names whose binary resolved on
	// the host PATH. Sorted for stable rendering.
	AvailableProviders []string

	// MissingProviders lists provider names that have an entry in
	// c.Providers but whose binary did not resolve. Sorted.
	MissingProviders []string

	// UnknownModels lists "provider/model" strings that survived the
	// availability filter but have no entry in the built-in
	// KnownProviders catalog. The Planner prompt renders them without
	// tier or summary; the user is responsible for the note.
	UnknownModels []string
}

// ResolveAvailability filters c.Roles options by host availability and
// returns a diagnostic report. The filter is always applied, even on
// roles whose options the user declared explicitly: bcc never spawns a
// binary that does not exist on PATH.
//
// Mutates c in place. Returns an error when any role ends up with an
// empty options list after filtering, naming the role and the missing
// provider.
//
// The check uses exec.LookPath. Tests can substitute the lookup by
// pre-populating PATH or by injecting a binary stub in a t.TempDir.
func ResolveAvailability(c *Config) (Availability, error) {
	return resolveAvailabilityWith(c, exec.LookPath)
}

// resolveAvailabilityWith is the testable kernel: same behavior, but
// the binary lookup is injectable. Production callers go through
// ResolveAvailability.
func resolveAvailabilityWith(c *Config, lookPath func(string) (string, error)) (Availability, error) {
	if c == nil {
		return Availability{}, nil
	}

	availableSet := map[string]bool{}
	missingSet := map[string]bool{}
	for name, p := range c.Providers {
		bin := p.Binary
		if bin == "" {
			bin = name
		}
		if _, err := lookPath(bin); err == nil {
			availableSet[name] = true
		} else {
			missingSet[name] = true
		}
	}

	av := Availability{
		AvailableProviders: sortedKeys(availableSet),
		MissingProviders:   sortedKeys(missingSet),
	}

	unknownSet := map[string]bool{}
	filterRole := func(role string, policy *RolePolicy) error {
		kept := make([]RoleOption, 0, len(policy.Options))
		for _, opt := range policy.Options {
			if !availableSet[opt.Provider] {
				slog.Warn("config: role option filtered (provider unavailable)",
					"role", role,
					"provider", opt.Provider,
					"model", opt.Model)
				continue
			}
			if _, ok := KnownModelByName(opt.Provider, opt.Model); !ok {
				key := opt.Provider + "/" + opt.Model
				if !unknownSet[key] {
					unknownSet[key] = true
					slog.Warn("config: role option references unknown model",
						"role", role,
						"provider", opt.Provider,
						"model", opt.Model,
						"hint", "tier and summary will be omitted from the Planner prompt")
				}
			}
			kept = append(kept, opt)
		}
		if len(kept) == 0 {
			return fmt.Errorf("config: role %s has no usable options after availability filter (available providers: %s)",
				role, strings.Join(av.AvailableProviders, ", "))
		}
		policy.Options = kept
		return nil
	}

	if err := filterRole("planner", &c.Roles.Planner); err != nil {
		return av, err
	}
	if err := filterRole("briefer", &c.Roles.Briefer); err != nil {
		return av, err
	}
	if err := filterRole("executor", &c.Roles.Executor); err != nil {
		return av, err
	}
	if err := filterRole("reviewer", &c.Roles.Reviewer); err != nil {
		return av, err
	}

	av.UnknownModels = sortedKeys(unknownSet)
	return av, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
