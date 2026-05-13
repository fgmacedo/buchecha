package config

// KnownProvider is bcc's built-in description of one provider vendor:
// the default binary name, baseline ExtraArgs, and the curated catalog
// of models bcc knows how to render in the Planner prompt.
//
// At config load time, ApplyDefaults seeds every known provider into
// Config.Providers so a user with an empty .bcc.toml gets a working
// multi-provider configuration constrained by what is actually
// installed on the host (see ResolveAvailability). Users override
// individual fields under [providers.<name>] in TOML.
type KnownProvider struct {
	Name      string
	Binary    string
	ExtraArgs []string
	Models    []ModelCapability
}

// ModelCapability is bcc's curated description of one model. Summary is
// a short (≤80 chars) hint focused on "when to use", rendered to the
// Planner so it can reason about per-phase routing without relying on
// memorized vendor lore. Tier is one of "fast" / "balanced" /
// "frontier". DefaultEfforts is the recommended efforts list bcc seeds
// into role options when the user does not declare any.
type ModelCapability struct {
	Provider       string
	Model          string
	Tier           string
	DefaultEfforts []string
	Summary        string
}

// knownProviders is bcc's exhaustive built-in registry. Adding a vendor
// means appending here plus shipping its adapter package; users opt in
// implicitly by either having the binary in PATH or declaring the
// matching [providers.<name>] section in .bcc.toml.
var knownProviders = []KnownProvider{
	{
		Name:      "claude",
		Binary:    "claude",
		ExtraArgs: nil,
		Models: []ModelCapability{
			{
				Provider:       "claude",
				Model:          "claude-opus-4-7",
				Tier:           "frontier",
				DefaultEfforts: []string{"high"},
				Summary:        "deep reasoning, architecture, non-trivial debugging",
			},
			{
				Provider:       "claude",
				Model:          "claude-sonnet-4-6",
				Tier:           "balanced",
				DefaultEfforts: []string{"medium"},
				Summary:        "default workhorse; mechanical work, layout, wiring",
			},
			{
				Provider:       "claude",
				Model:          "claude-haiku-4-5",
				Tier:           "fast",
				DefaultEfforts: []string{"low"},
				Summary:        "cheapest; for phases the Planner already briefed inline",
			},
		},
	},
	{
		Name:      "codex",
		Binary:    "codex",
		ExtraArgs: nil,
		Models: []ModelCapability{
			{
				Provider:       "codex",
				Model:          "gpt-5.5",
				Tier:           "frontier",
				DefaultEfforts: []string{"high"},
				Summary:        "deep reasoning, flagship; complex coding and research",
			},
			{
				Provider:       "codex",
				Model:          "gpt-5.3-codex",
				Tier:           "balanced",
				DefaultEfforts: []string{"medium"},
				Summary:        "coding-specialized; default workhorse for agentic tasks",
			},
			{
				Provider:       "codex",
				Model:          "gpt-5.4-mini",
				Tier:           "fast",
				DefaultEfforts: []string{"low"},
				Summary:        "cheapest; mechanical work, scaffolding, low-risk phases",
			},
		},
	},
}

// KnownProviderList returns a copy of the built-in provider registry.
// Callers should not mutate the returned slice; the lists inside each
// entry are also shared and must not be modified in place.
func KnownProviderList() []KnownProvider {
	out := make([]KnownProvider, len(knownProviders))
	copy(out, knownProviders)
	return out
}

// KnownProviderByName returns the KnownProvider with the given name and
// whether it exists in the built-in registry.
func KnownProviderByName(name string) (KnownProvider, bool) {
	for _, kp := range knownProviders {
		if kp.Name == name {
			return kp, true
		}
	}
	return KnownProvider{}, false
}

// KnownModelByName returns the curated ModelCapability for (provider,
// model) and whether it is in the registry. Used to look up tier and
// summary when rendering the Planner's options table; a false result
// means the user declared a model bcc has no curated metadata for, in
// which case the prompt renders the entry without tier/summary.
func KnownModelByName(provider, model string) (ModelCapability, bool) {
	for _, kp := range knownProviders {
		if kp.Name != provider {
			continue
		}
		for _, m := range kp.Models {
			if m.Model == model {
				return m, true
			}
		}
	}
	return ModelCapability{}, false
}
