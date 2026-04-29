package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var (
	initForce    bool
	initLanguage string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .bcc.toml in the current project (interactive wizard)",
	Long:  "Walk through an interactive wizard to generate .bcc.toml with project language, agent executor, spec location, loop settings, and env handling.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInitWizard(os.Stdin, os.Stdout, ".bcc.toml")
	},
}

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing .bcc.toml")
	initCmd.Flags().StringVar(&initLanguage, "language", "", "skip language prompt (en or pt-BR)")
	rootCmd.AddCommand(initCmd)
}

// initInput is the wizard's collected answers; also the input to
// WriteConfigTOML so tests can drive it directly without stdin scripting.
type initInput struct {
	Language        string
	Agent           string
	Binary          string
	Model           string
	SpecsDir        string
	Mode            string
	MaxIter         int
	BranchPrefix    string
	EnvFiles        []string
	SkipPermissions bool // explicit; default true by wizard
}

func runInitWizard(stdin io.Reader, stdout io.Writer, target string) error {
	if _, err := os.Stat(target); err == nil && !initForce {
		fmt.Fprintf(stdout, "%s already exists. Re-run with --force to overwrite.\n", target)
		return nil
	}

	r := bufio.NewReader(stdin)

	in := initInput{
		Language:        initLanguage,
		Agent:           "claude",
		Mode:            "phase",
		MaxIter:         20,
		BranchPrefix:    "feat",
		SpecsDir:        "docs/specs",
		EnvFiles:        []string{".env"},
		SkipPermissions: true,
	}

	if in.Language == "" {
		in.Language = ask(r, stdout, "Project language [en/pt-BR]", "en")
	}
	if in.Language != "en" && in.Language != "pt-BR" {
		return fmt.Errorf("invalid language %q (use en or pt-BR)", in.Language)
	}

	in.Agent = ask(r, stdout, "Agent [claude/codex/gemini/custom]", "claude")

	binSuggest := in.Agent
	if abs, err := exec.LookPath(in.Agent); err == nil {
		binSuggest = abs
	}
	in.Binary = ask(r, stdout, "Agent binary path", binSuggest)

	if in.Agent == "claude" {
		in.Model = ask(r, stdout, "Model", "claude-opus-4-7")
	}

	in.SpecsDir = ask(r, stdout, "Spec directory", in.SpecsDir)

	in.Mode = ask(r, stdout, "Loop mode [phase/single-shot]", in.Mode)
	if in.Mode != "phase" && in.Mode != "single-shot" {
		return fmt.Errorf("invalid loop mode %q", in.Mode)
	}

	maxIterStr := ask(r, stdout, "Max iterations", "20")
	if _, err := fmt.Sscanf(maxIterStr, "%d", &in.MaxIter); err != nil || in.MaxIter <= 0 {
		return fmt.Errorf("invalid max iterations %q", maxIterStr)
	}

	in.BranchPrefix = ask(r, stdout, "Branch prefix", in.BranchPrefix)

	envFilesStr := ask(r, stdout, "Env files (comma-separated)", ".env")
	in.EnvFiles = splitTrim(envFilesStr, ",")

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Skip agent permission prompts (autonomous mode)")
	fmt.Fprintln(stdout, "  bcc runs the agent in print mode without a TTY. To complete a phase end")
	fmt.Fprintln(stdout, "  to end without intervention, the agent must run shell commands, edits,")
	fmt.Fprintln(stdout, "  and writes WITHOUT being prompted for each one.")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "  Choosing 'yes' means: you accept that the agent will read, write, edit,")
	fmt.Fprintln(stdout, "  and execute commands inside the project directory autonomously.")
	fmt.Fprintln(stdout, "  Choosing 'no' is an opt-out: the loop will likely stall or degrade")
	fmt.Fprintln(stdout, "  because tool calls that need approval cannot be answered. Useful only")
	fmt.Fprintln(stdout, "  for dry-runs or agents without a permission system.")
	fmt.Fprintln(stdout, "")
	skipStr := ask(r, stdout, "Skip permission prompts? [yes/no]", "yes")
	switch strings.ToLower(strings.TrimSpace(skipStr)) {
	case "yes", "y":
		in.SkipPermissions = true
	case "no", "n":
		in.SkipPermissions = false
	default:
		return fmt.Errorf("invalid answer %q (use yes or no)", skipStr)
	}

	if err := WriteConfigTOML(target, in); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote %s\n", target)
	return nil
}

// WriteConfigTOML writes a .bcc.toml file at path with the provided
// settings. Exposed for tests; the wizard wraps it.
func WriteConfigTOML(path string, in initInput) error {
	var sb strings.Builder
	sb.WriteString("[project]\n")
	sb.WriteString(fmt.Sprintf("language = %q\n\n", in.Language))

	sb.WriteString("[executor]\n")
	sb.WriteString(fmt.Sprintf("agent = %q\n", in.Agent))
	sb.WriteString(fmt.Sprintf("binary = %q\n", in.Binary))
	if in.Model != "" {
		sb.WriteString(fmt.Sprintf("model = %q\n", in.Model))
	}
	sb.WriteString("extra_args = []\n")
	sb.WriteString("# skip_permissions=true tells the adapter to suppress the agent's permission\n")
	sb.WriteString("# prompts (claude maps this to --dangerously-skip-permissions). Required for\n")
	sb.WriteString("# the autonomous loop. Set to false for a dry-run; the loop is unlikely to\n")
	sb.WriteString("# converge in that mode. The user accepts the trade-off either way.\n")
	sb.WriteString(fmt.Sprintf("skip_permissions = %t\n\n", in.SkipPermissions))

	sb.WriteString("[specs]\n")
	sb.WriteString(fmt.Sprintf("dir = %q\n\n", in.SpecsDir))

	sb.WriteString("[loop]\n")
	sb.WriteString(fmt.Sprintf("mode = %q\n", in.Mode))
	sb.WriteString(fmt.Sprintf("max_iterations = %d\n\n", in.MaxIter))

	sb.WriteString("[git]\n")
	sb.WriteString(fmt.Sprintf("branch_prefix = %q\n", in.BranchPrefix))
	sb.WriteString("require_commit_per_iteration = true\n\n")

	sb.WriteString("[env]\n")
	sb.WriteString("files = [")
	for i, f := range in.EnvFiles {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%q", f))
	}
	sb.WriteString("]\n\n")
	sb.WriteString("[env.vars]\n")
	sb.WriteString("# Add per-project env vars here. Tilde and ${VAR} are expanded.\n")

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func ask(r *bufio.Reader, w io.Writer, prompt, def string) string {
	fmt.Fprintf(w, "%s (%s): ", prompt, def)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
