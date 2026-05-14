package config

import (
	"fmt"
	"log/slog"
)

// FilterProviders restricts every role's Options to a specific provider.
//
// globalProvider, when non-empty, applies to all four roles (planner,
// briefer, executor, reviewer). plannerProvider, when non-empty,
// overrides globalProvider for the planner role only.
//
// The filter is destructive: options whose Provider does not match the
// per-role target are removed from policy.Options. Order is preserved
// among surviving options. The Planner therefore receives role menus
// containing only the desired vendor, so it naturally assigns that
// vendor to every phase without any prompt change.
//
// Mutates c in place. Returns an error when any role ends up with an
// empty options list after filtering, naming the role and the rejected
// provider.
func FilterProviders(c *Config, globalProvider, plannerProvider string) error {
	if c == nil {
		return nil
	}
	if globalProvider == "" && plannerProvider == "" {
		return nil
	}

	targetFor := func(role string) string {
		if role == "planner" && plannerProvider != "" {
			return plannerProvider
		}
		return globalProvider
	}

	filterRole := func(role string, policy *RolePolicy) error {
		target := targetFor(role)
		if target == "" {
			return nil
		}
		kept := make([]RoleOption, 0, len(policy.Options))
		for _, opt := range policy.Options {
			if opt.Provider != target {
				slog.Warn("config: role option filtered (provider pin)",
					"role", role,
					"provider", opt.Provider,
					"model", opt.Model,
					"pinned", target)
				continue
			}
			kept = append(kept, opt)
		}
		if len(kept) == 0 {
			return fmt.Errorf("config: role %s has no options for provider %q (none of the available options match)",
				role, target)
		}
		policy.Options = kept
		return nil
	}

	if err := filterRole("planner", &c.Roles.Planner); err != nil {
		return err
	}
	if err := filterRole("briefer", &c.Roles.Briefer); err != nil {
		return err
	}
	if err := filterRole("executor", &c.Roles.Executor); err != nil {
		return err
	}
	if err := filterRole("reviewer", &c.Roles.Reviewer); err != nil {
		return err
	}
	return nil
}
