package config

import "slices"

// ApplyDefaults fills in zero-valued fields of c so an empty .bcc.toml
// produces a working configuration.
//
// Idempotent: explicit fields are never overwritten. Adding entries to
// the known-providers registry (known.go) automatically widens the
// defaults so a fresh codex/gemini binary on a host enables that
// provider out of the box, without any user opt-in.
func ApplyDefaults(c *Config) {
	if c.Project.Language == "" {
		c.Project.Language = "en"
	}

	if c.Journal.Store == "" {
		c.Journal.Store = "markdown_inspec"
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
			v := true
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
		c.Loop.MaxIterations = 20
	}
	if c.Loop.RetryBudget == 0 {
		c.Loop.RetryBudget = 2
	}

	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = "feat"
	}

	if len(c.Env.Files) == 0 {
		c.Env.Files = []string{".env"}
	}
}

// defaultPlannerOptions: frontier reasoning amortizes across the whole
// run; the Planner is the one place where deep thinking pays for itself
// many times over. One option keeps the choice deterministic.
func defaultPlannerOptions() []RoleOption {
	return []RoleOption{
		{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"high"}},
	}
}

// defaultBrieferOptions: balanced is plenty when the Briefer is invoked
// at all. Most phases ship with prepared_briefing inline by the Planner;
// the Briefer is the exception, used for phases whose briefing depends
// on runtime state.
func defaultBrieferOptions() []RoleOption {
	return []RoleOption{
		{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
	}
}

// defaultExecutorOptions: balanced as the default; frontier as an
// upgrade slot the Planner can reach for on architecturally-loaded
// phases, fast as a downgrade slot for phases the Planner already
// briefed inline. Three entries express the full "Planner can pay
// more or less when it needs to" idea across the tier spectrum.
func defaultExecutorOptions() []RoleOption {
	return []RoleOption{
		{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium", "high"}},
		{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"low", "medium", "high"}},
		{Provider: "claude", Model: "claude-haiku-4-5", Efforts: []string{"low", "medium", "high"}},
	}
}

// defaultReviewerOptions: audits are deterministic against acceptance
// criteria; frontier is rarely needed and trivial reviews can be
// skipped via skip_review on the Plan.
func defaultReviewerOptions() []RoleOption {
	return []RoleOption{
		{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
	}
}
