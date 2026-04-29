package config

// ApplyDefaults fills in zero-valued fields of c based on Project.Language.
// Idempotent: explicit fields are never overwritten.
//
// When Language is empty, "en" is used. Unknown languages fall back to "en"
// with the localized string fields left empty (they remain available for
// later override; never overwritten).
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

	if c.Executor.Agent == "" {
		c.Executor.Agent = "claude"
	}
	if c.Executor.Binary == "" {
		c.Executor.Binary = "claude"
	}
	if c.Specs.Dir == "" {
		c.Specs.Dir = "docs/specs"
	}
	if c.Loop.Mode == "" {
		c.Loop.Mode = "phase"
	}
	if c.Loop.MaxIterations == 0 {
		c.Loop.MaxIterations = 20
	}
	if c.Git.BranchPrefix == "" {
		c.Git.BranchPrefix = "feat"
	}
	if len(c.Env.Files) == 0 {
		c.Env.Files = []string{".env"}
	}
}

func applyDefaultsEn(c *Config) {
	if c.Specs.PlanHeading == "" {
		c.Specs.PlanHeading = "## Implementation Plan"
	}
	if c.Specs.JournalHeading == "" {
		c.Specs.JournalHeading = "## Execution Journal"
	}
	if c.Loop.Results.OK == "" {
		c.Loop.Results.OK = "ok"
	}
	if c.Loop.Results.Partial == "" {
		c.Loop.Results.Partial = "partial"
	}
	if c.Loop.Results.Done == "" {
		c.Loop.Results.Done = "done"
	}
	if c.Loop.Results.Blocked == "" {
		c.Loop.Results.Blocked = "blocked"
	}
}

func applyDefaultsPtBR(c *Config) {
	if c.Specs.PlanHeading == "" {
		c.Specs.PlanHeading = "## Plano de implementação"
	}
	if c.Specs.JournalHeading == "" {
		c.Specs.JournalHeading = "## Diário de execução"
	}
	if c.Loop.Results.OK == "" {
		c.Loop.Results.OK = "ok"
	}
	if c.Loop.Results.Partial == "" {
		c.Loop.Results.Partial = "parcial"
	}
	if c.Loop.Results.Done == "" {
		c.Loop.Results.Done = "finalizado"
	}
	if c.Loop.Results.Blocked == "" {
		c.Loop.Results.Blocked = "bloqueado"
	}
}
