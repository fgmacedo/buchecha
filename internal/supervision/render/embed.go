package render

import _ "embed"

//go:embed prompts/briefing.md
var briefingPromptMD string

//go:embed prompts/briefing_system.md
var briefingSystemMD string
