package config

// ApplyDefaults fills in zero-valued fields of c based on Project.Language.
// Idempotent: explicit fields are never overwritten.
//
// When Language is empty, "en" is used. Unknown languages fall back to
// "en" with the localized string fields left empty (they remain
// available for later override; never overwritten).
func ApplyDefaults(c *Config) {
	if c.Project.Language == "" {
		c.Project.Language = "en"
	}

	switch c.Project.Language {
	case "en":
		applyDefaultsEn(c)
	case "pt-BR":
		applyDefaultsPtBR(c)
	default:
		applyDefaultsEn(c)
	}

	// Spec selector + per-adapter defaults.
	if c.Spec.Format == "" {
		c.Spec.Format = "markdown_bcc"
	}
	if c.Spec.MarkdownBCC.Dir == "" {
		c.Spec.MarkdownBCC.Dir = "docs/specs"
	}

	// Journal selector. The journal store is purely a prompt input to
	// AgentBriefing; bcc never reads it.
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
	if c.Loop.Mode == "" {
		c.Loop.Mode = "phase"
	}
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
}

func applyDefaultsEn(c *Config) {
	if c.Spec.MarkdownBCC.PlanHeading == "" {
		c.Spec.MarkdownBCC.PlanHeading = "## Implementation Plan"
	}
	if c.Spec.MarkdownBCC.JournalHeading == "" {
		c.Spec.MarkdownBCC.JournalHeading = "## Execution Journal"
	}
}

func applyDefaultsPtBR(c *Config) {
	if c.Spec.MarkdownBCC.PlanHeading == "" {
		c.Spec.MarkdownBCC.PlanHeading = "## Plano de implementação"
	}
	if c.Spec.MarkdownBCC.JournalHeading == "" {
		c.Spec.MarkdownBCC.JournalHeading = "## Diário de execução"
	}
}
