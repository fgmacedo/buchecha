package config

import "slices"

// Default values exported so callers that need to echo the same defaults
// (e.g. the init wizard's starter values and prompt suggestions) share a
// single source of truth with ApplyDefaults instead of duplicating
// literals. Changing a value here changes the binary's default and the
// scaffolded .bcc.toml together.
const (
	DefaultLanguage        = "en"
	DefaultJournalStore    = "markdown_inspec"
	DefaultMaxIterations   = 20
	DefaultRetryBudget     = 3
	DefaultBranchPrefix    = "feat"
	DefaultSkipPermissions = true
)

// DefaultEnvFiles returns a fresh copy of the default env-file list.
// A function (rather than a package-level slice) keeps callers from
// aliasing and mutating the same backing array.
func DefaultEnvFiles() []string { return []string{".env"} }

// ApplyDefaults fills in zero-valued fields of c so an empty .bcc.toml
// produces a working configuration.
//
// Idempotent: explicit fields are never overwritten. Adding entries to
// the known-providers registry (known.go) automatically widens the
// defaults so a fresh codex/gemini binary on a host enables that
// provider out of the box, without any user opt-in.
func ApplyDefaults(c *Config) {
	if c.Project.Language == "" {
		c.Project.Language = DefaultLanguage
	}

	if c.Journal.Store == "" {
		c.Journal.Store = DefaultJournalStore
	}

	// Providers: seed every known provider with adapter-level defaults
	// so the role-options menu can reference them without each user
	// having to declare empty stubs. User declarations override field
	// by field.
	if c.Providers == nil {
		c.Providers = make(map[string]Provider, len(knownProviders))
	}
	for _, kp := range knownProviders {
		p := c.Providers[kp.Name]
		if p.Binary == "" {
			p.Binary = kp.Binary
		}
		if p.ExtraArgs == nil {
			p.ExtraArgs = slices.Clone(kp.ExtraArgs)
		}
		if p.SkipPermissions == nil {
			v := DefaultSkipPermissions
			p.SkipPermissions = &v
		}
		c.Providers[kp.Name] = p
	}

	// Roles: seed default options when the user did not declare any.
	// The defaults pick a sensible model+effort triple per role,
	// covering the common case ("Planner = frontier, Briefer/Reviewer
	// = balanced, Executor = balanced with a frontier fallback").
	if len(c.Roles.Planner.Options) == 0 {
		c.Roles.Planner.Options = defaultPlannerOptions()
	}
	if len(c.Roles.Briefer.Options) == 0 {
		c.Roles.Briefer.Options = defaultBrieferOptions()
	}
	if len(c.Roles.Executor.Options) == 0 {
		c.Roles.Executor.Options = defaultExecutorOptions()
	}
	if len(c.Roles.Reviewer.Options) == 0 {
		c.Roles.Reviewer.Options = defaultReviewerOptions()
	}

	if c.Loop.MaxIterations == 0 {
		c.Loop.MaxIterations = DefaultMaxIterations
	}
	if c.Loop.RetryBudget == 0 {
		c.Loop.RetryBudget = DefaultRetryBudget
	}

	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = DefaultBranchPrefix
	}

	if len(c.Env.Files) == 0 {
		c.Env.Files = DefaultEnvFiles()
	}
}

// defaultRoleTiers expresses the per-role philosophy in terms of model
// tiers, decoupled from any specific vendor or model name:
//
//   - Planner: frontier reasoning amortizes across the whole run.
//   - Briefer: balanced is plenty; most phases ship prepared_briefing inline.
//   - Executor: balanced as the default with frontier and fast as upgrade
//     and downgrade slots the Planner can reach for per phase. The tier
//     order is the Planner's preference: best-fit first.
//   - Reviewer: audits are deterministic against acceptance criteria;
//     balanced is enough.
//
// The actual RoleOption list is derived from the cross-product of these
// tiers and the knownProviders registry, so adding a new provider in
// known.go automatically widens every role menu without edits here.
var defaultRoleTiers = map[string][]string{
	"planner":  {"frontier"},
	"briefer":  {"balanced"},
	"executor": {"balanced", "frontier", "fast"},
	"reviewer": {"balanced"},
}

// defaultRoleOptions emits one RoleOption per (tier, provider, model)
// match, iterating tiers in policy order and knownProviders in registry
// order. When more than one provider has a model at the same tier
// (e.g. claude and codex both at balanced), both are included; the
// Planner picks per phase. efforts resolves the effort list to attach
// per model so we can keep curated narrow lists for the strict roles
// and widen them for the Executor.
func defaultRoleOptions(tiers []string, efforts func(ModelCapability) []string) []RoleOption {
	var out []RoleOption
	for _, tier := range tiers {
		for _, kp := range knownProviders {
			for _, m := range kp.Models {
				if m.Tier != tier {
					continue
				}
				out = append(out, RoleOption{
					Provider: kp.Name,
					Model:    m.Model,
					Efforts:  slices.Clone(efforts(m)),
				})
			}
		}
	}
	return out
}

// curatedEfforts uses the model's recommended effort list as-is. Right
// for roles where the Planner does not need flexibility per phase.
func curatedEfforts(m ModelCapability) []string { return m.DefaultEfforts }

// widenedEfforts ignores the curated default and offers the full effort
// spectrum. Right for the Executor, where the Planner uses effort as a
// per-phase dial to trade cost against depth.
func widenedEfforts(ModelCapability) []string { return []string{"low", "medium", "high"} }

func defaultPlannerOptions() []RoleOption {
	return defaultRoleOptions(defaultRoleTiers["planner"], curatedEfforts)
}

func defaultBrieferOptions() []RoleOption {
	return defaultRoleOptions(defaultRoleTiers["briefer"], curatedEfforts)
}

func defaultExecutorOptions() []RoleOption {
	return defaultRoleOptions(defaultRoleTiers["executor"], widenedEfforts)
}

func defaultReviewerOptions() []RoleOption {
	return defaultRoleOptions(defaultRoleTiers["reviewer"], curatedEfforts)
}
