package config

// ApplyDefaults fills in zero-valued fields of c based on Project.Language.
// Idempotent: explicit fields are never overwritten.
//
// When Language is empty, "en" is used. Unknown languages fall back to
// "en".
func ApplyDefaults(c *Config) {
	if c.Project.Language == "" {
		c.Project.Language = "en"
	}

	// Journal selector. The journal store is purely a prompt input; bcc
	// never reads the journal.
	if c.Journal.Store == "" {
		c.Journal.Store = "markdown_inspec"
	}

	// Agent selector + per-adapter defaults.
	if c.Agent.Name == "" {
		c.Agent.Name = "claude"
	}
	if c.Agent.Claude.Binary == "" {
		c.Agent.Claude.Binary = "claude"
	}
	if c.Agent.Claude.SkipPermissions == nil {
		v := true
		c.Agent.Claude.SkipPermissions = &v
	}

	// Loop defaults.
	if c.Loop.MaxIterations == 0 {
		c.Loop.MaxIterations = 20
	}

	// Git defaults.
	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = "feat"
	}

	// Env defaults.
	if len(c.Env.Files) == 0 {
		c.Env.Files = []string{".env"}
	}

	// Director defaults. RetryBudget=2 matches the spec; the Claude
	// binary defaults to PATH lookup.
	if c.Director.RetryBudget == 0 {
		c.Director.RetryBudget = 2
	}
	if c.Director.Claude.Binary == "" {
		c.Director.Claude.Binary = "claude"
	}
}
