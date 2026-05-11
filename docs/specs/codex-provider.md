# Codex provider + Provider abstraction

Adiciona suporte ao codex CLI (`codex-cli 0.130.0`) como segundo provider do bcc, ao lado de claude. A entrega inclui uma refatoração da abstração: hoje cada vendor tem dois packages (`internal/executor/<vendor>` + `internal/supervision/<vendor>`) que misturam papel (Planner/Briefer/Executor/Reviewer) com vendor e duplicam ~85% de plumbing (argv, mcp-config tempfile, parser, ringbuffer de stderr, cancelamento, emissão de SpawnStarted/Finished). A entrega consolida tudo em um único port `Provider` consumido por orquestradores de papel vendor-agnósticos, e então adiciona codex como uma casca fina.

Estilo: solo project, sem aliases de compat. Cada milestone deixa `go vet ./... && gofmt -l . && go test -race ./...` verdes. Cost-tracking do codex é best-effort em v1 (schema JSONL de `codex exec --json` não está documentado; parsear o que vier de amostra real, deixar zero quando ausente).

## Referência rápida do codex CLI

```
codex exec [--json] [--ignore-user-config] [--skip-git-repo-check] [--ephemeral] \
  [-c key=value]... \
  [-s read-only|workspace-write|danger-full-access] \
  [--ask-for-approval never] [--dangerously-bypass-approvals-and-sandbox] \
  [-m <model>] [-C <cwd>] [<PROMPT> | -]
```

MCP per-spawn via `-c 'mcp_servers.bcc.url="..."' -c 'mcp_servers.bcc.bearer_token="..."'` com `--ignore-user-config` para isolamento.

## P1: Provider port + spawnkit compartilhado

Cria o port vendor-agnóstico e extrai os helpers compartilhados que hoje vivem duplicados entre `internal/executor/claude/` e `internal/supervision/claude/`. Nada quebra: os pacotes antigos continuam coexistindo, apenas passam a importar do spawnkit nos próximos milestones.

### T1.1: criar `internal/provider/provider.go` [ ]

Definir o port `Provider` e os tipos de requisição/resposta. Conteúdo mínimo:

- `interface Provider { Name() string; Spawn(ctx, SpawnRequest) (SpawnResult, error) }`
- `SpawnRequest{ Role, Prompt, SystemPrompt, Model, Effort, Sandbox, AllowedTools, SkipPermissions, MaxBudgetUSD, ExtraArgs, MCP, AgentID, PhaseID, IterationID, Attempt, SessionStore, Events, LoopEvents }`
- `SpawnResult{ SpawnID, ExitCode, StderrTail, DurationMS, CostUSD, Tokens }` (CostUSD/Tokens best-effort)
- `MCPSpec{ URL, Token, ConnectionName }`
- `Sandbox` enum: `SandboxReadOnly | SandboxWorkspaceWrite | SandboxDangerFullAccess`

Sem implementação ainda. Doc comment em cada exportado.

Aceitação: `go build ./internal/provider/...` verde. Pacote compila sem nenhum import além de stdlib e dos packages domain já existentes (`internal/loop/agentcontract`, `internal/supervision/session`).

### T1.2: criar `internal/provider/registry.go` [ ]

Registry simples nome→Provider. API: `NewRegistry(providers ...Provider) *Registry`, `(*Registry).Get(name string) (Provider, bool)`, `(*Registry).Names() []string`. Sem registro automático via init().

Aceitação: tests unitários em `registry_test.go` cobrindo Get com nome conhecido, Get com nome desconhecido, ordem de Names estável.

### T1.3: extrair ringBuffer para `internal/provider/spawnkit/ringbuffer.go` [ ]

Mover o `ringBuffer` que hoje aparece duplicado em `internal/executor/claude/claude.go:49–71` e `internal/supervision/claude/claude.go:57–79`. Ainda não remover dos call sites originais; eles vão passar a usar este na próxima fase.

Aceitação: `internal/provider/spawnkit/ringbuffer_test.go` cobrindo write-around, tail size, comportamento quando buffer ainda não encheu.

### T1.4: extrair writeMCPConfig para `internal/provider/spawnkit/mcpconfig.go` [ ]

Mover `writeMCPConfig(url, token, connectionName)` que hoje vive em `internal/supervision/claude/mcpconfig.go`. Manter o mesmo schema JSON (mcpServers.bcc.type=http, url, headers Authorization/X-BCC-Role). API: `WriteMCPConfig(dir string, spec MCPSpec) (path string, cleanup func() error, err error)`.

Aceitação: test unitário verifica permissões 0o600, cleanup remove o tempdir, JSON resultante bate com o que claude consome hoje.

### T1.5: extrair persistência de prompt e emissão de SpawnStarted/Finished para `internal/provider/spawnkit/spawn_event.go` [ ]

Hoje em ambos os adapters claude: gera spawnID, escreve `.bcc/sessions/<id>/spawns/<spawnID>.md` (mode 0o600), emite `loop.SpawnStarted` antes do start e `loop.SpawnFinished` após o wait. API: `PersistPrompt(store *session.Store, spawnID, prompt string) (path string, err error)` e `EmitSpawnStarted(events chan<- loop.Event, info SpawnInfo)` / `EmitSpawnFinished(events chan<- loop.Event, info SpawnInfo, result SpawnResult)`.

Aceitação: tests usando um fake `chan loop.Event` confirmam que os eventos saem com os campos corretos e que persistência grava em path determinístico.

## P2: provider/claude — consolidação dos dois packages antigos (depends on P1)

Mescla `internal/executor/claude/` + `internal/supervision/claude/` em um único `internal/provider/claude/` com um único método `Spawn`. Move `streamjson/` junto. Atualiza todos os call sites neste mesmo milestone (sem aliases). Deleta os pacotes antigos.

### T2.1: criar `internal/provider/claude/claude.go` com `Spawn` único [ ]

Adapter que constrói argv claude a partir de SpawnRequest, escreve mcp-config via spawnkit, spawna subprocess, parseia stream-json via `internal/provider/claude/streamjson` (próxima task), extrai cost/tokens do `result_summary`, emite SpawnStarted/Finished via spawnkit, devolve SpawnResult. Mapping de campos:

- `--mcp-config <tempfile> --strict-mcp-config` se `MCP.URL != ""`
- `--system-prompt-file <tempfile>` se `SystemPrompt != ""`; nesse caso `Prompt` vai por stdin
- `--dangerously-skip-permissions` se `SkipPermissions`
- `--allowed-tools <csv>` se `len(AllowedTools) > 0`
- `--model`, `--effort`, `--max-budget-usd` quando setados
- `-p --output-format stream-json --verbose` sempre
- `Sandbox` ignorado (claude não tem sandbox CLI; controle é via permissions+allowed-tools)

Aceitação: `claude.New(cfg) provider.Provider` retorna struct que satisfaz o port. Cancelamento via ctx mata o subprocess com SIGINT→SIGKILL (CancelGrace default 5s). StderrTail é a tail do ringBuffer.

### T2.2: mover `internal/executor/claude/streamjson/` para `internal/provider/claude/streamjson/` [ ]

Movimentação pura: package, tipos, parser, helpers `LastResultSummary`. Atualizar imports. Tests do package vêm junto.

Aceitação: `go test ./internal/provider/claude/streamjson/...` verde com os mesmos casos.

### T2.3: atualizar todos os call sites para o novo adapter [ ]

Em `internal/cli/run_director.go` (linhas ~318-325 e ~1030-1120 conforme exploração anterior):

- Substituir `directorclaude.New(directorclaude.Config{...})` que retornava algo implementando Planner+Briefer+Reviewer por construção de um `provider.Registry` contendo `claude.New(...)`. Os papéis em si ainda usam o package legado neste commit; só o executor passa a usar `provider.Registry`.
- Substituir `claude.New(claude.Config{...})` na factory `makeNewExecutor` por chamada ao novo `provider.Claude.Spawn` via wrapper temporário (introduzido como `ProviderExecutor` em P4; aqui ainda é wrapper inline ou stub equivalente).

A migração full dos roles vem em P3; aqui só o Executor migra. Aceitação intermediária OK.

Aceitação: build verde; `go test -race ./...` verde; uma run real (`./bcc run testdata/specs/diag-dag.md`) completa sem regressão.

### T2.4: deletar `internal/executor/claude/` e `internal/supervision/claude/` [ ]

Após T2.3 e P3 (DirectorRoles vendor-agnóstico) os dois packages ficam órfãos. Deletar com `git rm -r`. Procurar imports residuais com `grep -r "internal/executor/claude\|internal/supervision/claude" .` e confirmar zero hits fora dos próprios arquivos removidos.

Aceitação: `go build ./...` verde sem os pacotes; nenhum import sobrando.

## P3: DirectorRoles vendor-agnóstico (depends on P2)

Move Planner/Briefer/Reviewer para acima da linha vendor. Cria `internal/supervision/director.go` com `DirectorRoles{provider}` implementando os três ports usando `Provider.Spawn`.

### T3.1: criar `internal/supervision/director.go` com `DirectorRoles` [ ]

Tipo `DirectorRoles` com construtor `NewDirectorRoles(registry *provider.Registry, cfg DirectorConfig) *DirectorRoles` onde DirectorConfig carrega `MaxBudgetUSD` e `AllowedTools` (default `["Read","Bash","Grep","Glob"]`). Implementa:

- `Plan(ctx, in PlannerInput, events) (*Plan, *SpawnStats, error)`
- `Brief(ctx, in BrieferInput, events) (*SpawnStats, error)`
- `Review(ctx, in ReviewerInput, events) (*SpawnStats, error)`

Cada método: monta prompt via `composePrompt`, escolhe provider via `registry.Get(in.Assignment.Provider)`, chama `Spawn` com `Sandbox=ReadOnly`, `AllowedTools=cfg.AllowedTools`, `SkipPermissions=true`, demais campos vindos do input. Budget check pós-spawn no orquestrador (`if cfg.MaxBudgetUSD > 0 && result.CostUSD > cfg.MaxBudgetUSD { return ErrBudgetExceeded }`).

Aceitação: `DirectorRoles` satisfaz `supervision.Planner`, `supervision.Briefer`, `supervision.Reviewer`. Tests com `provider/fake` cobrem cada método: feliz path, budget exceeded, erro do provider, cancelamento.

### T3.2: mover `composePrompt`, `planView`, `briefView`, `reviewView` para `internal/supervision/director.go` [ ]

Esses helpers hoje vivem em `internal/supervision/claude/claude.go:397+`. Movem como estão (já são vendor-neutros: só consomem templates de `internal/supervision/render` e partials de `internal/loop/agentcontract`).

Aceitação: nenhum import a partir de packages vendor. Tests do prompt composto preservados.

### T3.3: cabear `run_director.go` para usar DirectorRoles [ ]

Substituir a construção de `directorclaude.New(...)` por:

```go
registry := provider.NewRegistry(claude.New(...))  // codex entra em P6
directorRoles := supervision.NewDirectorRoles(registry, supervision.DirectorConfig{
    MaxBudgetUSD: cfg.Providers["claude"].MaxBudgetUSD, // até codex entrar
    AllowedTools: []string{"Read","Bash","Grep","Glob"},
})
adapter := directorRoles  // satisfaz Planner+Briefer+Reviewer
```

Aceitação: `bcc run testdata/specs/diag-dag.md` completa idêntico ao antes da refatoração (golden test no `mcp-log.jsonl` se já existe; senão, comparação manual antes/depois).

## P4: ProviderExecutor (depends on P3)

Adapta `provider.Provider` para `loop.Executor` (assinatura `Run(ctx, prompt, events) (ExecResult, error)`).

### T4.1: criar `internal/loop/executor.go` com `ProviderExecutor` [ ]

Tipo `ProviderExecutor{ Provider provider.Provider, Request provider.SpawnRequest }` onde Request é um template (sem Prompt) pré-preenchido na factory. `Run` faz `req := p.Request; req.Prompt = prompt; res, err := p.Provider.Spawn(ctx, req); return ExecResult{ExitCode: res.ExitCode, StderrTail: res.StderrTail, SpawnID: res.SpawnID}, err`.

Aceitação: tests com `provider/fake` confirmam ExitCode, StderrTail e propagação de ctx.Cancel.

### T4.2: refazer `makeNewExecutor` em `run_director.go` [ ]

Remover o `if assignment.Provider != "claude" { return failingExecutor }`. Substituir por:

```go
prov, ok := registry.Get(assignment.Provider)
if !ok { return failingExecutor("unknown provider: " + assignment.Provider) }
return &loop.ProviderExecutor{
    Provider: prov,
    Request: provider.SpawnRequest{
        Role:            "executor",
        Sandbox:         provider.SandboxWorkspaceWrite,
        SkipPermissions: providerCfg.ShouldSkipPermissions(),
        ExtraArgs:       providerCfg.ExtraArgs,
        Model:           assignment.Model,
        Effort:          assignment.Effort,
        SystemPrompt:    systemPromptText,
        MCP:             mcpSpec,
        AgentID:         mcpCfg.AgentID,
        PhaseID:         args.PhaseID,
        IterationID:     args.BriefingID,
        Attempt:         args.Attempt,
        SessionStore:    store,
        LoopEvents:      loopEvents,
    },
}
```

`renderSystem` continua produzindo o conteúdo do system prompt; passa como `SystemPrompt string` em vez de `SystemPromptFile`. O claude adapter materializa em tempfile internamente quando for invocá-lo.

Aceitação: build verde; `bcc run testdata/specs/diag-dag.md` completa.

## P5: provider/codex skeleton (depends on P4)

Adapter codex implementando o mesmo port. Antes de implementar, capturar amostra real de `codex exec --json` para conhecer o schema. Cost/tokens best-effort.

### T5.1: capturar amostra de `codex exec --json` [ ]

Rodar à mão (em um diretório scratch fora do repo, ou com `--skip-git-repo-check`):

```bash
codex exec --json --ask-for-approval never -s read-only \
  --skip-git-repo-check --ephemeral -C /tmp \
  "liste os arquivos do diretório atual e descreva em uma linha cada"
```

Salvar a saída JSONL em `internal/provider/codex/jsonl/testdata/sample-readonly.jsonl`. Rodar uma segunda variante com `-s workspace-write` que cria um arquivo (`echo "criar um arquivo /tmp/codex-sample.txt com conteúdo 'hi'"`); salvar como `testdata/sample-tool-use.jsonl`. Documentar event types observados em `testdata/README.md`.

Aceitação: dois arquivos JSONL não-vazios e listagem dos `type` distintos observados (`agent_message`, `tool_call`, `tool_call_result`, `task_complete`, etc.).

### T5.2: criar parser `internal/provider/codex/jsonl/parse.go` [ ]

Função `ParseLine(line []byte, now time.Time) ([]agentcontract.AgentEvent, error)` modelada como o parser claude. Mapear:

- `agent_message` → `KindAssistantText`
- `tool_call` (function calls e MCP calls) → `KindToolUse`; se `name` começa com `mcp__bcc__` o caller (loop) trata
- `tool_call_result` → `KindToolResult`
- `task_complete` ou `result` final → `KindResultSummary` com `TotalCostUSD`, `Tokens`, `DurationMS` extraídos best-effort (campos podem não existir; deixar zero)
- types desconhecidos: log slog.Debug + drop

Tests com fixtures de T5.1: round-trip parse, contagens esperadas por tipo, último ResultSummary acessível.

Aceitação: `go test ./internal/provider/codex/jsonl/...` verde. Cobertura dos types observados em testdata.

### T5.3: criar `internal/provider/codex/codex.go` com `Spawn` [ ]

Adapter codex: monta argv conforme tabela:

```go
args := []string{"exec", "--json", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral"}
// MCP via -c overrides
if req.MCP.URL != "" {
    args = append(args,
        "-c", fmt.Sprintf(`mcp_servers.bcc.url=%q`, req.MCP.URL),
        "-c", fmt.Sprintf(`mcp_servers.bcc.bearer_token=%q`, req.MCP.Token),
    )
    // se schema suportar headers customizadas:
    // -c 'mcp_servers.bcc.headers={"X-BCC-Role"="<role>"}'
}
// sandbox
switch req.Sandbox {
case provider.SandboxReadOnly:        args = append(args, "-s", "read-only")
case provider.SandboxWorkspaceWrite:  args = append(args, "-s", "workspace-write")
case provider.SandboxDangerFullAccess: args = append(args, "-s", "danger-full-access")
}
// permissions
if req.SkipPermissions {
    args = append(args, "--ask-for-approval", "never")
}
if req.Model != ""  { args = append(args, "-m", req.Model) }
// effort do bcc não tem mapeamento direto no codex 0.130; ignorar em v1 (documentar)
// cwd inherited (não setar -C)
args = append(args, req.ExtraArgs...)
// prompt via stdin com SystemPrompt prepended
fullPrompt := req.SystemPrompt
if fullPrompt != "" { fullPrompt += "\n\n---\n\n" }
fullPrompt += req.Prompt
```

Resto idêntico em forma ao claude adapter: spawnkit para mcp/ringbuffer/spawn-events, parser do T5.2 lendo stdout. Subprocess via `exec.CommandContext` com `Stdin = strings.NewReader(fullPrompt)`, args `... -` no final para indicar stdin.

Aceitação: `provider.Codex.Spawn` satisfaz o port; test usando fixture (capturando subprocess via stub binary que cospe um JSONL de exemplo) confirma SpawnResult.ExitCode, StderrTail, e que eventos chegam no channel.

### T5.4: verificar como codex aceita `mcp_servers.<name>.headers` em `-c` overrides [ ]

Investigar antes de fechar T5.3: rodar `codex mcp add bcc-test --url http://localhost:9999/mcp/` e inspecionar `~/.codex/config.toml` para ver o schema gerado. Confirmar se há suporte a headers customizadas (para `X-BCC-Role`). Se não houver, abrir nota no código indicando que o role label só vai no path da URL (ou via bearer_token único por papel) até codex suportar headers.

Aceitação: comentário no `codex.go` documentando o schema observado. `~/.codex/config.toml` restaurado ao estado original (remover entry bcc-test).

## P6: codex no registry + wizard (depends on P5)

Registra codex como provider conhecido, faz o bcc detectá-lo no PATH e oferece nas opções de role.

### T6.1: adicionar codex em `internal/config/known.go` [ ]

Apend a `knownProviders`:

```go
{
    Name:      "codex",
    Binary:    "codex",
    ExtraArgs: nil,
    Models: []ModelCapability{
        // nomes exatos a confirmar via `codex debug models --bundled` antes de gravar
        {Provider: "codex", Model: "gpt-5.4",       Tier: "frontier",  DefaultEfforts: []string{"high"},   Summary: "deep reasoning, codex's flagship"},
        {Provider: "codex", Model: "gpt-5.4-codex", Tier: "balanced",  DefaultEfforts: []string{"medium"}, Summary: "coding-specialized; default workhorse"},
        {Provider: "codex", Model: "gpt-5-mini",    Tier: "fast",      DefaultEfforts: []string{"low"},    Summary: "cheapest; mechanical work"},
    },
},
```

Antes de gravar, rodar `codex debug models --bundled` e ajustar os IDs para os exatos no catalog. Se um tier não existir, omitir.

Aceitação: `KnownProviderByName("codex")` retorna true; `KnownModelByName("codex", "gpt-5.4-codex")` retorna ok.

### T6.2: registrar codex adapter no Registry em `run_director.go` [ ]

```go
registry := provider.NewRegistry(
    claude.New(claudeCfg),
    codex.New(codexCfg),
)
```

Onde `codexCfg` consome `cfg.Providers["codex"]` igual claude faz. Se o binário não está no PATH (checado por `config.ResolveAvailability`), o provider ainda é registrado mas vai falhar no Spawn (com erro claro). O Planner já filtra options indisponíveis antes de rotear.

Aceitação: com `[providers.codex]` declarado no `.bcc.toml`, `bcc run` aceita assignments para codex sem erro de "unknown provider".

### T6.3: atualizar wizard `bcc init` para listar codex quando detectado [ ]

Em `internal/cli/init.go` (ou onde o wizard pergunta sobre providers): listar codex como opção quando `codex` está no PATH. Mesma UX que claude: pergunta sobre `skip_permissions`, sobre allowed models do menu de roles.

Aceitação: `bcc init` num projeto novo com codex instalado gera `.bcc.toml` com `[providers.codex]` populado e ao menos uma entry em `[roles.executor].options` apontando para codex.

## P7: validação e2e e docs (depends on P6)

Smoke test multi-provider e atualização da documentação interna.

### T7.1: rodar `bcc run` multi-provider [ ]

Criar `.bcc.toml` temporário com:

```toml
[[roles.executor.options]]
provider = "claude"
model    = "claude-sonnet-4-6"
efforts  = ["medium"]

[[roles.executor.options]]
provider = "codex"
model    = "gpt-5.4-codex"
efforts  = ["medium"]
```

Rodar `./bcc run testdata/specs/diag-dag.md`. Verificar no `bcc sessions show <id>`:
- Pelo menos um spawn com `provider=codex` aparece (Planner escolhe).
- Working tree fica limpo entre iterações.
- Cost chip da TUI exibe codex (mesmo que com zeros).
- `mcp-log.jsonl` mostra o handler aceitando calls do codex (X-BCC-Role correto).

Aceitação: spec termina com `done` em todas as tasks; nenhuma task vai para `escalate`.

### T7.2: atualizar `CLAUDE.md` [ ]

Editar `/Users/fernando.macedo/projects/buchecha/CLAUDE.md`:

- Atualizar a seção "Layers" trocando `internal/executor/<adapter>/` e `internal/supervision/<vendor>/` por `internal/provider/<vendor>/`.
- Atualizar a regra "OCP: adicionar um novo agent vendor é um novo package sob `executor/` e `supervision/<vendor>/`" para "novo package sob `internal/provider/<vendor>/`".
- Adicionar nota sobre `Sandbox` enum no Provider port (campo que claude ignora hoje mas codex usa).

Aceitação: nenhum trecho do CLAUDE.md aponta para caminhos que não existem mais.

### T7.3: atualizar guides em `docs/guides/` [ ]

`docs/guides/director.md` e `docs/guides/director.pt-BR.md` mencionam `internal/supervision/claude/`. Trocar pelas novas paths e pela ideia de Provider+roles separados.

Aceitação: `grep -r "internal/executor/claude\|internal/supervision/claude" docs/` retorna zero hits.

### T7.4: salvar especificação executada [ ]

A própria spec (`docs/specs/codex-provider.md`) fica como registro normativo. Não criar PRD adicional; commit message documenta a migração com link para esta spec.

Aceitação: arquivo presente, sem tasks pendentes `[ ]` ao final da execução (todas viraram `[x]` à medida que o Reviewer aprova).
