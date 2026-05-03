---
title: "Spec: Reviewed execution implementation"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-02
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - director
  - reviewed-execution
  - implementation
comments: true
---

# Spec: Reviewed execution implementation

## Summary

Implementação técnica da [PRD 2: Reviewed execution](./2026-04-30-reviewed-execution.md). Introduz o papel do Director dentro do bcc com três capacidades: planejar (gerar um DAG tipado de fases), informar (briefar o Executor por fase) e revisar (auditar a saída do Executor contra critérios de aceite). O Director comunica com o bcc por uma superfície de ferramentas tipadas (no espírito MCP); cada saída é um payload validado e persistido. Esta spec é executada autonomamente pelo próprio bcc, fase por fase.

## Context

A PRD descreve o porquê. Esta spec descreve **o quê** e **onde**. Seguimos a arquitetura hexagonal-light já consolidada no repositório (`internal/loop/`, `internal/loop/agentcontract/`, `internal/format/markdown_bcc/`, `internal/executor/claude/`). O Director ganha um pacote de domínio próprio (`internal/director/`) com portas definidas no consumidor e adapters em sub-pacotes irmãos.

A pesquisa de superfícies do Claude Code está em [research: Claude integration surfaces](./2026-04-30-research-claude-integration-surfaces.md). Decisões já tomadas:

1. **Channels rejeitados** como transporte do Director (research preview, login claude.ai, sem entrega em `-p`). Bloqueador documentado na seção 3 do research doc.
2. **Director roda em `claude -p`** com `--json-schema` validando o payload tipado, `--bare` e `--no-session-persistence` para chamadas baratas e isoladas.
3. **Briefing chega ao Executor** como `--system-prompt-file`. A retomada com feedback usa `--fork-session` para não poluir a sessão original.
4. **Permission relay** continua via `--dangerously-skip-permissions` nesta spec; a substituição por `--permission-prompt-tool` fica fora de escopo (PRD diz "estritamente mais forte", mas é uma evolução separada).

## Goals and non-goals

### Goals

- [x] G1: Pacote `internal/director/` com tipos canônicos (`Plan`, `Phase`, `AcceptanceItem`, `Briefing`, `Verdict`, `VerdictFeedback`), serialização JSON e persistência em `.bcc/{plan,briefings,verdicts}/`.
- [x] G2: Adapter Director Claude (`internal/director/claude/`) implementando `Planner`, `Briefer` e `Reviewer`, todas instrumentadas com cost reporting.
- [x] G3: `bcc run --director <spec>` que planeja, confirma com o usuário, e executa o ciclo brief/execute/review até `done`.
- [x] G4: Decider rescrito para tratar verdicts do Director como autoritativos; retry budget configurável por fase com escalação.
- [x] G5: Painel TUI de estado do Director (fase ativa, briefing resumido, verdict, custo).
- [x] G6: `bcc run --resume` reconstrói estado a partir de `.bcc/{plan,briefings,verdicts}/` sem replanejar quando o hash do spec é o mesmo.

### Non-goals

- Channels, MCP server bcc-channel, plugin Claude (rejeitados ou fora de escopo).
- Substituir `--dangerously-skip-permissions` por `--permission-prompt-tool` (spec separada).
- Execução paralela de fases (PRD 3).
- Atribuição capability-aware (PRD 4); `executor_assignment` fica como campo opcional do `Phase`, ignorado pelo loop até a PRD 4.
- Multi-vendor: apenas o adapter Claude é entregue. Codex/Gemini ficam para depois; a porta é vendor-neutral por construção.
- Re-escrita do `Validator` da PRD 1; esta spec não depende dela em runtime.

## Proposal

### Package layout

```
internal/director/                  domínio + portas (stdlib-only)
├── director.go                     comentário de pacote, doc do contrato
├── types.go                        Plan, Phase, AcceptanceItem, Briefing, Verdict, ...
├── ids.go                          PhaseID estável (sha256(spec_hash + intent))
├── store.go                        WritePlan/ReadPlan, ...; FS abstraction via io/fs
├── store_test.go                   round-trip + corruption + missing files
├── ports.go                        Planner, Briefer, Reviewer interfaces
├── prompts/
│   ├── plan.md                     embedded; system prompt do Planner
│   ├── brief.md                    embedded; system prompt do Briefer
│   └── review.md                   embedded; system prompt do Reviewer
├── schemas/
│   ├── plan.schema.json            JSON Schema; passado a `claude --json-schema`
│   ├── briefing.schema.json
│   └── verdict.schema.json
└── claude/                         adapter
    ├── claude.go                   Planner/Briefer/Reviewer implementados
    ├── claude_test.go              fakes + golden output
    └── testdata/
        ├── plan_response.json
        └── verdict_response.json

internal/director/fake/             fake adapter para testes do loop
└── fake.go                         scripted Planner/Briefer/Reviewer

internal/loop/                      mudanças aqui em P7
├── director_state.go               estado do Director vivo no loop
├── director_decider.go             novo decider que consome Verdict
└── ...                             eventos novos: PhasePlanned, PhaseBriefed, ...
```

### Configuration

Adiciona uma sub-tabela `[director]` ao `.bcc.toml`. Defaults aplicados em `internal/config/defaults.go`.

```toml
[director]
enabled = true                      # default-on tristate; --no-director ou enabled = false desliga
retry_budget = 2                    # default por fase; phase.retry_budget no plan sobrescreve

[director.claude]
binary = "claude"                   # default: PATH lookup
model = ""                          # vazio = default do binário
extra_args = []
max_budget_usd = 0                  # 0 = sem cap; > 0 vira --max-budget-usd
```

### Wire protocol additions

Nenhuma mudança no wire do Executor. O Executor reporta progresso e resultado de cada iteração chamando tools MCP (`mcp__bcc__task_started`, `mcp__bcc__task_completed`, `mcp__bcc__iteration_result`) registradas pelo servidor in-process do bcc; cada chamada chega ao adapter como `tool_use` no stream-json, é traduzida em `BccEvent` por `agentcontract.FromToolCall` e roteada como `loop.KindBccEvent`. O Director comunica com o bcc por **stdout JSON validado contra schema** (uma chamada `claude --json-schema <arquivo>` por papel). Não há sentinelas adicionais sobre stdin do Director nem sobre stdout do Executor.

### Eventos do Loop

Novos eventos no canal `loop.Event` (todos satisfazem `isLoopEvent()`), emitidos apenas em modo Director:

1. `PhasePlanned{Plan, At}`: o Director devolveu um `Plan`; o usuário ainda não confirmou.
2. `PhaseBriefed{PhaseID, Attempt, Briefing, At}`: briefing pronto, Executor prestes a rodar.
3. `PhaseReviewed{PhaseID, Attempt, Verdict, At}`: verdict pronto, decider vai consumir.
4. `DirectorEscalation{PhaseID, Attempt, Reasoning, At}`: retry exausto; loop pausa esperando input.

### Loop state machine (modo Director)

```
plan → confirm → for each pending phase:
  brief → execute → review → decide
                                 ├── approve → next phase
                                 ├── revise (attempt < budget) → brief (with feedback)
                                 ├── revise (attempt == budget) → escalate
                                 └── escalate → user prompt → resume | skip | abort
done when every phase has an approved verdict.
```

A PRD diz: o `iteration_result=done` do Executor é informativo; o decider só sinaliza `done` quando todas as fases do plano têm verdict `approve`.

### Director protocol (typed tool surface)

Cada papel é uma chamada `claude -p --bare --no-session-persistence --json-schema <file>` com prompt fixo + payload de entrada inline. A resposta é o único objeto JSON na stdout, validado pelo schema; bcc consome e descarta o resto.

| Role | Schema | Input | Output |
|---|---|---|---|
| Planner | plan.schema.json | spec content + spec hash | `Plan` |
| Briefer | briefing.schema.json | phase + plan + prior verdicts (resumidos) | `Briefing` |
| Reviewer | verdict.schema.json | phase + briefing + diff + journal entry + acceptance evidence | `Verdict` |

### Persistência

Todos os arquivos sob `.bcc/`. Layout:

```
.bcc/
├── plan.json                       Plan + spec_hash + planned_at
├── briefings/<phase-id>-<attempt>.json
└── verdicts/<phase-id>-<attempt>.json
```

`.bcc/` continua sendo a única raiz de estado do bcc; o adapter Director não escreve em outro lugar.

### Security e segurança

1. **Director nunca relaxa as restrições absolutas**. O Verdict não pode dar ao Executor permissões que o framework proíbe. O renderer de Briefing concatena (não substitui) o bloco `absolute_restrictions` do `agentcontract`.
2. **Director é read-only no working tree**. As únicas escritas que o adapter faz são em `.bcc/`. Validado por teste: o adapter recebe um working dir read-only no test e a chamada de plano/review tem que sucesso.
3. **Cost cap obrigatório quando configurado**. `[director.claude].max_budget_usd > 0` traduz para `--max-budget-usd`; a chamada falha "fail-closed" (não abre fase) se a Claude exceder.
4. **Nenhum log de valores de env**. Adapters seguem a regra do `CLAUDE.md`: nomes apenas.
5. **Spec hash sha256(bytes) calculado em uma única passada**; nunca um hash de texto formatado, para não falhar por mudanças cosméticas inexistentes.

## Implementation Plan

Cada fase entrega valor isolado e tem critérios de aceite verificáveis em `go test`/`go build`. Fases dependem somente do que está explicitamente declarado em "Depends on".

### Phase 1: Director domain types and persistence

**Context:** Antes de qualquer adapter ou integração com o loop, o domínio do Director precisa existir como tipos puros e persistência funcional. Tudo aqui é stdlib-only, sem dependências externas, e não importa nada de `internal/executor/`, `internal/format/`, `internal/loop/`. Esta fase inclui o `Plan`, `Phase`, `AcceptanceItem`, `Briefing`, `Verdict`, `VerdictFeedback` e a camada de leitura/escrita em `.bcc/`. Sem esta base, P2 a P9 não compilam.

**Depends on:** nada.

**Acceptance:**

1. `go test ./internal/director/...` passa em verde sob `-race`, exercitando round-trip JSON e leitura de fixtures.
2. `internal/director/` não importa nenhum pacote sob `internal/executor/`, `internal/format/`, `internal/loop/`, `internal/configloader/`.
3. `PhaseID` derivado do spec hash + intent é estável: o mesmo `(hash, intent)` produz o mesmo ID em invocações diferentes; specs diferentes produzem IDs diferentes.

**Tasks:**

1. [x] Criar `internal/director/director.go` (apenas comentário de pacote descrevendo o propósito e regras de fronteira) e `internal/director/types.go` com structs `Plan`, `Phase`, `AcceptanceItem`, `Briefing`, `Verdict`, `VerdictFeedback`, `RequiredChange`, `OutOfScopeNote`. Campos exatamente como na schema ilustrativa da PRD; `executor_assignment` permanece como `*ExecutorAssignment` opcional para a PRD 4.
1. [x] Definir tipos enumerados como strings: `VerdictOutcome` (`approve`/`revise`/`escalate`), `EvidenceKind` (`diff`/`test`/`build`/`manual`); cada um com `String()` e `MarshalJSON`/`UnmarshalJSON` validando o conjunto fechado. Round-trip golden test cobrindo cada valor.
1. [x] Criar `internal/director/ids.go` com `func PhaseID(specHash string, intent string) string` retornando `sha256(specHash + "\x00" + intent)` em hex truncado para 16 caracteres. Teste de estabilidade e teste de colisão entre (hash, intent) diferentes.
1. [x] Criar `internal/director/store.go` com `Store` struct sobre `string baseDir` (e.g. `.bcc/`) expondo `WritePlan(*Plan)`, `ReadPlan() (*Plan, error)`, `WriteBriefing(*Briefing)`, `ReadBriefing(phaseID string, attempt int)`, `WriteVerdict(*Verdict)`, `ReadVerdict(...)`, `LatestVerdict(phaseID string)`. Cada `Write*` cria dirs faltantes (`MkdirAll 0o755`); todo `Read*` retorna `fs.ErrNotExist` envelopado quando ausente.
1. [x] Adicionar função `SpecHash(content []byte) string` em `internal/director/ids.go`: sha256 hex completo. Caso de teste: dois bytes idênticos produzem o mesmo hash; bytes com BOM diferem.
1. [x] Tabela de testes em `store_test.go`: round-trip com `t.TempDir()`, leitura de arquivo ausente, leitura de JSON corrompido (deve retornar erro envelopado, nunca panicar), `LatestVerdict` percorrendo `attempt` 1..N e retornando o maior.
1. [x] Adicionar verificação de fronteira: `internal/director/director.go` lista no comentário os pacotes proibidos; um teste em `director_test.go` (placeholder) garante via `go list -deps ./internal/director` que nenhum desses imports apareça (executado pelo CI; pode ser inicialmente um teste shell-out com `go list`).

### Phase 2: Director config and CLI flag

**Context:** Antes de cabear adapters, o usuário precisa de um caminho ergonômico para ligar o Director: `--director` no CLI ou `[director].enabled = true` no `.bcc.toml`. Esta fase também adiciona `[director.claude]` com `max_budget_usd` e `retry_budget` global. Nenhuma execução real ainda; o cabeamento só decide se o caminho Director é tomado mais à frente.

**Depends on:** P1 (referencia `director.RetryBudget` se for definido como tipo; evite acoplar, prefira `int` puro neste momento).

**Acceptance:**

1. `bcc run --director <spec>` aceita a flag e propaga até a função de execução; `bcc run` sem ela mantém o comportamento MVP idêntico, validado por `internal/cli/run_test.go`.
2. `[director]` e `[director.claude]` são parseados pelo loader TOML existente sem mudar o decoder.
3. `bcc run --director` em uma config sem `[director]` aplica defaults (`retry_budget=2`, `max_budget_usd=0`).

**Tasks:**

1. [x] Adicionar `Director DirectorConfig` em `internal/config/config.go` com `Enabled bool`, `RetryBudget int`, `Claude DirectorClaude`. `DirectorClaude` carrega `Binary string`, `Model string`, `ExtraArgs []string`, `MaxBudgetUSD float64`.
1. [x] Atualizar `internal/config/defaults.go` para preencher `RetryBudget=2` e `Claude.Binary="claude"` quando ausentes. Teste em `defaults_test.go`.
1. [x] Adicionar flag `--director` ao `runCmd` em `internal/cli/run.go`. Quando `--director` é passado, `cfg.Director.Enabled = true` (override sobre TOML).
1. [x] No início de `runSpec`, ramificar: se `cfg.Director.Enabled`, tomar o caminho `runDirector(...)` (stub que retorna `errors.New("director not yet wired")` nesta fase); senão, manter o caminho atual. Teste verifica a ramificação via flag.
1. [x] Atualizar `bcc init` (`internal/cli/init.go`) para escrever `[director]` e `[director.claude]` com defaults comentados. Não interativo nesta fase; segunda volta no wizard fica fora de escopo.
1. [x] Atualizar `internal/configloader/toml/toml_test.go` com fixture cobrindo `[director]` e `[director.claude]`.

### Phase 3: Director ports and Claude adapter scaffolding

**Context:** Define o contrato programático que o restante das fases consome. As portas vivem no consumidor (`internal/director/ports.go`); o adapter `internal/director/claude/` implementa cada porta delegando ao binário `claude` em modo `-p --bare --no-session-persistence --json-schema <arquivo>`. Esta fase entrega Planner/Briefer/Reviewer como interfaces, o adapter Claude inicial cobrindo as três operações com testes baseados em fakes (não invocamos o binário real ainda; isso vem em P4-P6 nos testes de integração).

**Depends on:** P1.

**Acceptance:**

1. `var _ director.Planner = (*director_claude.Adapter)(nil)` (e idem para `Briefer`, `Reviewer`) compila.
2. Testes unitários em `internal/director/claude/claude_test.go` exercitam: montagem de argumentos da CLI; parse e validação do payload de retorno; falha quando JSON é inválido; falha quando tokens excedem o cap configurado.
3. Os três prompts (`plan.md`, `brief.md`, `review.md`) e os três schemas (`*.schema.json`) embarcam via `//go:embed` e estão acessíveis pelo adapter.

**Tasks:**

1. [x] Criar `internal/director/ports.go` com:
   - `type Planner interface { Plan(ctx context.Context, in PlannerInput) (*Plan, *DirectorCallStats, error) }`
   - `type Briefer interface { Brief(ctx context.Context, in BrieferInput) (*Briefing, *DirectorCallStats, error) }`
   - `type Reviewer interface { Review(ctx context.Context, in ReviewerInput) (*Verdict, *DirectorCallStats, error) }`
   - `DirectorCallStats { DurationMS int64; CostUSD float64; InputTokens, OutputTokens int64 }` para cost reporting (NFR3).
1. [x] Definir `PlannerInput`, `BrieferInput`, `ReviewerInput`: structs com somente os dados que cada papel precisa. Planner recebe `SpecPath`, `SpecContent []byte`, `SpecHash string`. Briefer recebe `*Plan`, `phaseID string`, `attempt int`, `priorVerdicts []*Verdict`, `priorFeedback *VerdictFeedback`. Reviewer recebe `*Plan`, `*Briefing`, `diff string`, `journalDelta string`, `acceptanceEvidence map[string]string`.
1. [x] Criar `internal/director/prompts/{plan,brief,review}.md` com o prompt operacional do papel, instruindo (a) ler somente as entradas do payload (sem leitura de filesystem além do `SpecPath` quando aplicável), (b) emitir um único objeto JSON conforme o schema, (c) seguir as `absolute_restrictions` (compostas via partial; reusa `agentcontract.Partials()`).
1. [x] Criar `internal/director/schemas/{plan,briefing,verdict}.schema.json` (JSON Schema 2020-12) modelando os tipos da P1. Validar via `go test` rodando `jsonschema.Compile(...)` sobre os arquivos (dependência: `github.com/santhosh-tekuri/jsonschema/v6`, novo go.mod entry; alternativa stdlib-only: validação por unmarshal estrito + verificações manuais. Preferir a dep externa por economia de manutenção; só ela na lista de deps novas).
1. [x] Criar `internal/director/claude/claude.go` com `Adapter` struct exportando `New(cfg Config) *Adapter`. `Config` carrega `Binary`, `Model`, `ExtraArgs`, `MaxBudgetUSD`, `Stderr io.Writer`, `CancelGrace time.Duration`.
1. [x] Implementar método interno `runJSONCall(ctx, schemaPath string, prompt string) ([]byte, *DirectorCallStats, error)`: monta `claude -p --bare --no-session-persistence --output-format stream-json --json-schema <path> [--model <m>] [--max-budget-usd <n>] <prompt>`, lê stdout linha a linha, captura o último `result` para extrair custo/tokens (reaproveita `parseResult` mental do `internal/executor/claude/claude.go`), encontra o objeto JSON do schema na sequência stream-json, retorna bytes + stats. Cancelamento espelha `executor/claude/claude.go` (SIGINT + WaitDelay).
1. [x] Implementar `Plan`, `Brief`, `Review` chamando `runJSONCall` com o prompt e schema apropriados; serializar o `*Input` em um payload JSON inline anexado ao prompt; deserializar a resposta para o tipo de domínio; retornar.
1. [x] Adicionar fakes em `internal/director/fake/fake.go`: `FakePlanner`, `FakeBriefer`, `FakeReviewer` com slots `PlanFn`, `BriefFn`, `ReviewFn` (funções que o teste escreve). Isso será o substrato para o teste de loop em P7.

### Phase 4: Plan generation and user confirmation

**Context:** Ponto de entrada do modo Director. Gera o plano, persiste, mostra ao usuário e espera confirmação antes do primeiro brief. Re-planejamento com diff fica em P9.

**Depends on:** P2, P3.

**Acceptance:**

1. `bcc run --director <spec>` lê o spec, chama `Planner.Plan`, persiste em `.bcc/plan.json`, e bloqueia esperando `[P]roceed`/`[A]bort` no stdin (ou `--auto-proceed` aprova sem bloquear). `[A]bort` retorna `ExitInvalid` sem rodar o Executor.
2. Plano com zero fases é rejeitado com erro claro (`director: planner returned an empty plan`); ExitInvalid.
3. Persistência inclui spec_hash; rerun com spec idêntico e `--resume` (P9) reutiliza; sem `--resume`, replaneja.

**Tasks:**

1. [x] Criar `internal/cli/run_director.go` com `func runDirector(ctx, cancel, specPath, cfg)` (chamado a partir do branch da flag em P2).
1. [x] Em `runDirector`: ler spec content, calcular `SpecHash`, instanciar `Adapter` Director Claude com `cfg.Director.Claude`, instanciar `Store` em `.bcc/`, chamar `Planner.Plan`, persistir.
1. [x] Renderer de plano: nova função em `internal/cli/render.go` que imprime `Plan` em texto (heading, success_criteria, tabela de fases). Em modo TUI o painel terá variante visual em P8; aqui texto na stderr é suficiente para a confirmação.
1. [x] Bloco de confirmação: lê uma linha de `os.Stdin`; aceita `p`/`P`, `a`/`A`. Flag `--auto-proceed` salta. Em modo TUI (`--output tui`), o input vem por uma `tea.Cmd` no Model (cabeamento mínimo nesta fase: sair do TUI durante a confirmação, igual ao padrão `[e]` no `runWithTUI`; refinamento em P8).
1. [x] Validação de plano: se `len(plan.Phases) == 0`, abortar; se houver `phase.depends_on` referenciando IDs ausentes, abortar. Função pura `ValidatePlan(*Plan) error` em `internal/director/types.go`, com testes.
1. [x] Spec hash calculado a partir do conteúdo lido do disco; persiste no `Plan.SpecHash` (campo novo no struct, adicionado em P1 se ainda não estiver ou em ajuste retroativo aqui com migração de schema).
1. [x] Teste de integração em `internal/cli/run_director_test.go` usando `internal/director/fake.FakePlanner` para devolver um plano scriptado; verifica que `.bcc/plan.json` é criado e a confirmação `p\n` desbloqueia.

### Phase 5: Briefing pipeline

**Context:** Para cada fase pendente do plano, o Briefer produz um `Briefing`; o briefing é renderizado em um arquivo system-prompt e o Executor é invocado com `--system-prompt-file`. O loop ainda não tem o ciclo brief/execute/review fechado, foco aqui é entregar uma fase ao Executor com o briefing certo e capturar a saída.

**Depends on:** P3, P4.

**Acceptance:**

1. Dado um plano e uma fase pendente, `Briefer.Brief` produz `Briefing` que é persistido em `.bcc/briefings/<phase-id>-1.json` e materializado em `.bcc/briefings/<phase-id>-1.prompt.md`.
2. O prompt materializado contém o spec excerpt pré-computado, scope, acceptance, e o `prior_feedback` quando attempt > 1; partials de `agentcontract` (wire protocol, absolute restrictions, working tree) ficam concatenados, jamais omitidos.
3. O Executor existente (`internal/executor/claude/`) aceita o caminho do prompt e roda; chamada de teste em `internal/cli/run_director_test.go` verifica que o subprocesso é chamado com `--system-prompt-file <path>` (em vez do prompt inline atual).

**Tasks:**

1. [x] Adicionar `BriefingFor(plan *Plan, phaseID string, attempt int) (*BrieferInput, error)` em `internal/director/`, montando o input com priors (lê `Store.LatestVerdict` para attempt > 1).
1. [x] Criar `internal/director/render.go` com `func RenderBriefingPrompt(*Briefing, *Phase) (string, error)`: usa `text/template` + `agentcontract.Partials()` para concatenar (a) cabeçalho com fase/attempt/scope, (b) acceptance criteria com IDs, (c) `prior_feedback` (renderizado em prosa: lista de `required_changes` e `out_of_scope`), (d) os três partials (wire_protocol, absolute_restrictions, working_tree).
1. [x] Estender `internal/executor/claude/claude.go` para aceitar `Config.SystemPromptFile string`; quando preenchido, o adapter passa `--system-prompt-file <path>` e omite o prompt positional. Default zero mantém o comportamento atual. Teste em `claude_test.go`.
1. [x] Em `runDirector`, fechar um ciclo "uma fase só" (placeholder até P7): para a primeira fase pendente, chamar `Briefer.Brief`, persistir, renderizar prompt em `.bcc/briefings/<phase-id>-<attempt>.prompt.md`, instanciar `Executor` com `SystemPromptFile=<path>`, rodar uma iteração. Sem review ainda (P6/P7).
1. [x] Spec excerpt: `BrieferInput` carrega o spec inteiro mas o Briefer recebe instrução de incluir somente o trecho relevante em `Briefing.SpecExcerpt`. Não fazemos slicing no bcc-side.
1. [x] Teste de golden: `RenderBriefingPrompt` contra um `*Briefing` fixo produz uma string estável; arquivo em `internal/director/testdata/briefing_golden.md`.

### Phase 6: Review pipeline

**Context:** Após o Executor sair tendo invocado `mcp__bcc__iteration_result` com `value=review` (a PRD redefine quando sinalizar `review`: ao terminar uma fase do plano, e não apenas em gates pelo observador), o Director roda o Reviewer. O Reviewer reúne evidências por critério de aceite (diff, test, build, manual), produz `Verdict`, persiste.

**Depends on:** P3, P5.

**Acceptance:**

1. Dado um diff de teste e um `Briefing` correspondente, `Reviewer.Review` retorna um `Verdict` válido (passes pelo schema) e bcc persiste em `.bcc/verdicts/<phase-id>-<attempt>.json`.
2. Para cada `AcceptanceItem.Evidence == "test"` ou `"build"`, o Reviewer **deve** consultar a evidência: o adapter Director Claude executa o agente passando o diff + uma instrução de rodar `go test` / `go build` localmente; o resultado entra em `acceptance_results[].note`. Para `manual`, o `note` é "manual; deferred to user via reasoning".
3. Verdict com `outcome=revise` carrega `feedback` não vazio; `outcome=approve` exige todos os `acceptance_results.passed=true`; `outcome=escalate` carrega `reasoning` com a razão. Validação ao parse, com erro claro.

**Tasks:**

1. [x] `func GatherDiff(ctx, git GitProbe, baseSHA, headSHA string) (string, error)` no `internal/director/`: porta nova `GitDiff` definida no consumidor (ou método extra em `loop.GitProbe`; preferir o último porque a porta já é o probe do working tree). Implementa em `internal/git/cli/cli.go`.
1. [x] `func GatherJournalDelta(specBefore, specAfter []byte) string`: extrai diferença textual da seção `## Execution Journal`. Pure function; teste com spec antes/depois.
1. [x] `Reviewer.Review` chamado com `ReviewerInput{Plan, Briefing, Diff, JournalDelta, AcceptanceEvidence}`. AcceptanceEvidence é um `map[string]string` (key = `AcceptanceItem.ID`, value = log/output capturado para evidências `test`/`build`); o adapter Claude obtém isso instruindo o próprio agent (o Reviewer **roda como agente**, não como bcc-side runner; bcc só lhe entrega o diff e o briefing e confia que ele executa).
1. [x] `ValidateVerdict(*Verdict) error`: verifica os invariantes (approve sem failed; revise com feedback; escalate com reasoning). Em `types.go`, com tabela de testes.
1. [x] Em `runDirector` (placeholder de P5), substituir o "uma fase só" por: rodar Executor → quando `signal == review` ou `signal == done`, gather diff + journal delta + chamar Reviewer → persistir verdict. Sem retry/escalate ainda.
1. [x] Atualizar `internal/format/markdown_bcc/contract.md`: na seção "Procedure" do modo loop, em modo Director o agente **deve invocar `mcp__bcc__iteration_result` com `value=review`** ao final de cada fase (em vez de `continue`). A escolha de modo é injetada via novo campo `templateData.DirectorEnabled bool`. Esta é a única mudança no contrato; a alternativa é não tocar e tratar `signal=continue` como "fase em progresso", mas o decider fica simples se review marca fim-de-fase.

### Phase 7: Loop integration and decider rewrite

**Context:** Junta tudo: state machine `brief → execute → review → decide` com retry budget, escalação, e tratamento de spec done apenas quando todas as fases têm verdict approve. O decider existente (`internal/loop/decider.go`) é estendido (não substituído) com um `DirectorDecider` que recebe `Verdict` e retorna `Action`. O `Loop.Run` atual ramifica em modo Director.

**Depends on:** P5, P6.

**Acceptance:**

1. `bcc run --director <spec>` em um spec com 3 fases mock (via fakes Director e Executor) executa as 3 fases sequencialmente e termina com `ExitDone`.
2. Quando o Reviewer fake retorna `revise` na primeira tentativa de uma fase, a fase é re-briefada (attempt=2) com `prior_feedback`; segundo attempt aprova; loop continua.
3. Quando o Reviewer fake retorna `revise` em todas as tentativas dentro do retry budget, o loop pausa em `DirectorEscalation` e espera input. Em texto/JSON mode, o input vem de stdin; em TUI vem do painel (cabeamento em P8).
4. `go test -race ./...` verde com integration test cobrindo aprovar / revise+approve / escalate / abort fluxos.

**Tasks:**

1. [x] Criar `internal/loop/director_decider.go` com `DirectorDecide(in DirectorDeciderInput) DirectorDecision`:
   - Input: `Verdict`, `attempt`, `retryBudget`, `headAdvanced bool`.
   - Output: `DirectorAction` (`AdvancePhase`, `RetryPhase`, `Escalate`, `Abort`, `CompleteSpec`) + `ExitCode` quando aplicável.
   - HEAD-stuck primeiro (espelha o decider atual): se Executor não commitou, sempre abort com `ExitHEADStuck`, independente do verdict.
   - Tabela de teste exaustiva (verdict outcome × attempt × budget × headAdvanced). 12 a 20 casos.
1. [x] Estender `loop.Loop` com campo opcional `Director DirectorPorts` (struct: `Planner`, `Briefer`, `Reviewer`, `Store`). Quando `Director != nil`, `Run` toma o caminho Director. Caminho legado preservado.
1. [x] Implementar o caminho Director em `loop.Loop.Run`:
   ```
   plan = readOrPlan()                # consulta Store; replaneja se hash diverge
   for phase in pendingPhases(plan):
       for attempt = 1..budget+1:
           briefing = Briefer.Brief(...)
           prompt = render(briefing)
           runExecutor(prompt)
           verdict = Reviewer.Review(diff, journal, briefing)
           switch DirectorDecide(...).Action:
             case AdvancePhase: break  # próxima fase
             case RetryPhase: continue
             case Escalate: emit DirectorEscalation; wait user input via PauseGate
             case Abort: return ExitInvalid
       if all approved: emit LoopFinished{Done}
   ```
1. [x] Reusar `PauseGate` (já existe em `loop.Loop`) para a escalação: a gate é canalizada do TUI; em modo texto/JSON, um `os.Stdin` reader em goroutine resolve via `[r]esume`/`[s]kip`/`[a]bort`.
1. [x] Emitir `PhasePlanned`, `PhaseBriefed`, `PhaseReviewed`, `DirectorEscalation` no canal `events` para que renders consumam.
1. [x] Integration test em `internal/loop/director_integration_test.go` usando fakes (todos: `director.FakePlanner/Briefer/Reviewer`, `loop.fakeExecutor`, `loop.fakeGitProbe`); cobre os fluxos da acceptance.

### Phase 8: TUI Director panel and cost reporting

**Context:** O Director é invisível no MVP TUI atual. Esta fase cria um painel dedicado mostrando estado vivo: plano (com checkboxes derivados de verdicts), fase ativa, briefing resumido (intent + scope + acceptance), verdict mais recente, custo acumulado por papel. Cobre G5 e parcialmente NFR3 (cost reporting na UI).

**Depends on:** P7.

**Acceptance:**

1. Em `bcc run --director <spec>` com `--output tui`, um novo painel "Director" mostra: phases `[x]/[/]/[ ]/[!]` (approved/in-progress/pending/escalated), fase ativa em destaque, custo acumulado.
2. Painel "Health" do TUI atual ganha uma linha "director cost: $X.XX" com soma de `DirectorCallStats.CostUSD` por iteração.
3. Em escalação, um overlay modal mostra `verdict.reasoning` e os botões `[R]esume`, `[S]kip`, `[A]bort`. Input do usuário é roteado para o `PauseGate` correto.

**Tasks:**

1. [x] Criar `internal/tui/director.go` com Model do painel: estado `plan *Plan`, `currentPhaseID string`, `latestVerdict *Verdict`, `cumulativeCost float64`. Renderer em lipgloss respeitando `--no-color`.
1. [x] Cabear painel ao Update existente: novo case para `PhasePlanned`, `PhaseBriefed`, `PhaseReviewed`, `DirectorEscalation`, `AgentEventReceived` (para custos por iteração).
1. [x] Layout: na grade atual, alocar slot para o painel Director quando o modo está ligado (Layout decide a partir de uma flag no Options). Quando desligado, layout MVP intacto.
1. [x] Modal de escalação: bubbletea state que captura todas as keys até o usuário escolher; `R`/`S`/`A` enviam ao `PauseGate` o token semântico correspondente. Estende `tui.Gate` com canais separados ou um único canal carregando `EscalationReply` (preferir o último).
1. [x] Atualizar `runWithTUI` em `internal/cli/run.go` para construir o Model com o `*Plan` carregado e os canais Director.
1. [x] Snapshot tests em `director_test.go` com cenários: plano fresco, fase em progresso, fase escalada, plano todo aprovado.

### Phase 9: Resume, spec-change replan, and documentation

**Context:** Polimento que fecha os requisitos NFR5 (resume) e FR4 (re-plan on spec change). Inclui documentação operacional e atualização do índice da iniciativa.

**Depends on:** P7, P8.

**Acceptance:**

1. `bcc run --director --resume <spec>` em uma sessão interrompida reconstrói plano + verdicts existentes e retoma da próxima fase pendente. Se o `SpecHash` divergir do persistido, replaneja e mostra `[D]iff [P]roceed [A]bort` ao usuário.
2. Editar o spec mid-run cancela a sessão Executor em curso (se a edição invalida a fase ativa), pede confirmação do novo plano, e reinicia a fase afetada.
3. `docs/specs/director/index.md` atualizado: PRD 2 marcada como tendo spec de implementação, link para esta spec.

**Tasks:**

1. [x] Adicionar flag `--resume` ao `runCmd`. Em `runDirector`, se `--resume`: ler `.bcc/plan.json`, comparar `Plan.SpecHash` com `SpecHash(currentSpecBytes)`; igual = continuar; diferente = `RePlanFlow`.
1. [x] `RePlanFlow`: chamar Planner novamente; calcular diff (`PlanDiff(old, new) PlanDiff` puro: phases adicionadas, removidas, modificadas); renderizar; pedir confirmação `[D]iff/[P]roceed/[A]bort`. Persistir o novo plano apenas em proceed.
1. [x] File watcher mid-run: deferido conforme o próprio task autoriza ("Se complexo, deixar para follow-up e documentar"). fsnotify ainda não está no `go.mod`, e a semântica de "edição invalida a fase ativa" é não trivial (a fase pode ter editado o spec ela mesma para escrever o journal). Sem cobertura runtime; o usuário edita, encerra o run, e roda `bcc run --director --resume <spec>` para retomar com o `RePlanFlow`.
1. [x] `PlanDiff` e renderer em `internal/director/diff.go`; tabela de testes. Suporte a saída texto + JSON.
1. [x] Atualizar `docs/specs/director/index.md` adicionando referência a esta spec na tabela "Documents in this initiative".
1. [x] Atualizar `CLAUDE.md` se necessário. Não foi necessário: a regra atual sobre `internal/loop/` cobre o caso, e `internal/director/director_test.go::TestImports` documenta a fronteira em código.
1. [x] Adicionar `docs/guides/director.md` curto: o que `--director` faz, como interpretar verdicts, como retomar, como configurar `[director]`. En e pt-BR sob `docs/guides/director{,.pt-BR}.md`.

## Cross-cutting requirements

Aplicáveis a todas as fases. Falha em qualquer um destes é gate de qualidade:

1. **Layer boundaries**: nenhum import de adapter em `internal/director/` (somente sub-pacotes `internal/director/<adapter>/` importam o pacote pai; o pai jamais importa filhos). Verificado em CI por `go list -deps`.
2. **Stdlib-only no domínio**: `internal/director/` (não-adapter) não importa nenhuma dependência externa, exceto a lib de JSON Schema na P3 (que é o único item adicionado ao `go.mod`).
3. **Tests pass com `-race`**: todos os tests novos devem passar sob `go test -race ./...` no CI.
4. **Cost cap fail-closed**: quando `MaxBudgetUSD > 0` e a chamada Director retornaria custo acima, a chamada falha com erro tipado (`ErrBudgetExceeded`); o loop mata a fase e escalona. Teste em P3.
5. **Nenhum log de valores de env**. Adapters seguem a regra do CLAUDE.md.
6. **`absolute_restrictions` jamais relaxadas**. Briefing renderer (P5) garante a presença do partial; teste de golden valida.
7. **gofmt + go vet limpos**. Aplicado a cada commit (já é regra do projeto).

## Done criteria

A spec está done quando:

1. Todas as fases P1–P9 têm seus checkboxes marcados.
2. `bcc run --director docs/specs/test-validation/<short-spec>.md` (ou um spec curto criado para self-test) completa com `ExitDone` em uma máquina com `claude` no PATH e API key válida.
3. `go test -race ./...` em verde.
4. `go vet ./...` sem alertas.
5. README ou `docs/guides/director.md` documenta como ligar e usar `--director`.

## Stop criteria

O agente para e aguarda observador quando:

1. Validação falha 3 vezes seguidas após `git revert` da última iteração problemática (regra padrão do contrato).
2. Ambíguo na PRD que requer decisão humana: emitir `value=blocked` com a pergunta.
3. Custo da chamada Director excede `[director.claude].max_budget_usd` em uma fase de teste e não é claro se o cap deveria ser elevado: emitir `value=blocked`.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Schema do payload Director (P3) muito rígido faz Claude falhar a validação de `--json-schema` | Schemas começam permissivos (campos opcionais ampliados); apertam em iterações conforme observação. Cada falha de schema vira fixture e teste. |
| Re-planejamento mid-run gera divergência entre verdicts antigos e novos phase IDs | `PhaseID` deriva de `(SpecHash, Intent)`; re-plan com mesmo intent reusa ID, verdicts antigos continuam válidos. Verdicts cujo phaseID some são arquivados em `.bcc/verdicts/_orphan-<id>.json`. |
| Budget caps ainda assim explodem em fases longas | NFR4: latência > 30s do Director emite `WARNING`; cumulativo no painel TUI. Usuário decide cancelar manualmente. |
| Reviewer roda testes que fazem write em arquivos do projeto fora da fase atual | Não há mitigação framework-level confiável neste recorte; documentar e confiar nas `absolute_restrictions`. PRD 4 + permission-prompt-tool entram em uma spec separada com mitigação real. |
| Determinismo (NFR6) não atinge 100% por variância do modelo | Documentar como "best-effort"; reruns idênticos não são contrato. |

## References

- [PRD 2: Reviewed execution](./2026-04-30-reviewed-execution.md): a PRD que esta spec implementa.
- [Initiative index](./index.md): contexto Director geral.
- [Research: Claude Code integration surfaces](./2026-04-30-research-claude-integration-surfaces.md): superfícies do Claude Code; canais rejeitados na seção 3, `--json-schema`/`--bare`/`--no-session-persistence`/`--system-prompt-file`/`--max-budget-usd`/`--fork-session` na seção 5.
- [PRD 1: Spec validation gate](./2026-04-30-spec-validation-gate.md): independente; pode rodar antes desta sem coupling.
- [PRD 4: Capability-aware execution](./2026-04-30-capability-aware-execution.md): define `ExecutorAssignment` que entra como campo opcional no `Phase` desta spec.
- `internal/loop/agentcontract/`: wire protocol e partials de markdown reusados pelo Briefing renderer.
- `internal/format/markdown_bcc/contract.md`: contrato do Executor; alterado minimamente em P6.

## Execution Journal

### 2026-05-02 19:30, Director default-on + planning feedback (post-P9 polish)

Two UX corrections after the first end-to-end run by the observer.

**Default flipped to on.** Before this entry the user had to pass `--director` on every invocation. The spec body, the `bcc init` template, and the `ApplyDefaults` defaults all encoded "opt-in". The Director is the standard loop now; the observer should not type a flag for the default. `DirectorConfig.Enabled` migrated from `bool` to `*bool` (tristate, mirroring `AgentClaude.SkipPermissions`); a new `IsEnabled()` accessor returns true when nil. `bcc init` writes `enabled = true`; `ApplyDefaults` keeps `Enabled` nil so absent TOML still resolves to true. Opt-out is `enabled = false` in `.bcc.toml` or the new `--no-director` CLI flag (`--director` and `--no-director` are mutually exclusive; bcc errors out if both are set).

**Planner feedback during the silent wait.** `claude -p --bare --json-schema` blocks for 30s-3min while the model thinks; the adapter drains stdout silently looking for the validated JSON object, and `--bare` claude emits almost nothing on stderr. The observer saw the bcc startup line and then nothing for minutes. `runDirectorWith` now prints `bcc: director enabled; planning <spec> (model thinking, typically 30s-3min)...` immediately, then `startPlanningHeartbeat` ticks `bcc: planner still working (Xs elapsed)` every 15s until the planner returns. First heartbeat at 15s (not immediately), so quick plans stay quiet.

- **Decisions**: tristate over `bool` so `enabled = false` in `.bcc.toml` survives an `ApplyDefaults` pass that would otherwise re-enable it. The absent-vs-explicit-false distinction is what makes default-on safely overridable without breaking on round-trip.
- **Decisions**: heartbeat lives in `cli/run_director.go` (host concern), not in the director Claude adapter. The adapter stays focused on subprocess + JSON extraction; UX feedback is composed at the call site so future adapters (codex, gemini) inherit it for free when they go through the same `runDirectorWith` path.
- **Decisions**: TUI mode still launches *after* the plan + confirmation. The dashboard shows from the first `IterationStarted`. Moving the plan render and confirmation into the TUI is a P5+ refactor (plan-confirmation modal panel) that does not block this fix.
- **Discovered**: stale spec text and tests referenced the old `bool` Enabled field and the "opt-in" framing. Updated `Configuration` block, `internal/cli/init.go` template, `init_test.go`, `defaults_test.go`, `toml_test.go`, plus the Director protocol section in this spec.

### 2026-05-02 18:00, Phase 9: Resume, spec-change replan, and documentation

`--resume` cabeado no `runCmd` e em `runDirector`. O caminho de resolução de plano migrou para `resolveDirectorPlan` em `internal/cli/run_director.go`, que ramifica três cenários: hash inalterado (reusa `.bcc/plan.json`, pula confirmação), hash divergente (chama `rePlanFlow`, computa `PlanDiff`, prompt `[D]iff/[P]roceed/[A]bort`), sem plano persistido (cai no caminho fresh padrão). `internal/director/diff.go` traz `PlanDiff`, `ComputePlanDiff`, `RenderPlanDiff` (texto) e `MarshalPlanDiffJSON` puros. Index da iniciativa atualizado e guia de operador em `docs/guides/director{,.pt-BR}.md`. `go test -race ./...` verde, `go vet` limpo.

- **Decisions**: `resolveDirectorPlan` é um helper com a mesma `directorDeps`/`directorIO` que `runDirectorWith` já carrega; sem novo struct ou injeção. A ramificação (resume vs fresh) fica em uma função, e `runDirectorWith` perdeu duas dúzias de linhas. `freshPlan` extrai o kernel "planner → SpecHash override → ValidatePlan" reusado pelo caminho fresh e pelo `rePlanFlow` para evitar duplicação.
- **Decisions**: no caminho `--resume + spec_hash igual`, **a confirmação é pulada inteira** (mesmo sem `--auto-proceed`). A premissa: o usuário já aprovou esse plano numa sessão anterior; pedir confirmação de novo trata o resume como replanejamento, o que mente sobre o que aconteceu. Se o usuário quer revisitar o plano, ele edita o spec (forçando hash mismatch) ou roda sem `--resume`.
- **Decisions**: `[A]bort` no re-plan flow **não sobrescreve** `.bcc/plan.json`. O plano antigo continua no disco para inspeção; um teste pina isso (`TestRunDirectorWith_ResumeHashMismatch_AbortPath`). Sobrescrever em abort daria a impressão de que o run sequer começou, o que contraria o ciclo de vida do artefato (foi escrito por uma sessão de sucesso anterior).
- **Decisions**: a UX do `[D]iff` é "re-renderizar o diff que já foi mostrado" em vez de mostrar diff completo (ex.: full diff de phases unchanged). O diff inicial já é apresentado antes do prompt; `[D]iff` é um atalho de "scroll back" para terminais que cortaram o output. Implementação: `promptDirectorRePlanConfirmation` recebe o `*PlanDiff` e chama `RenderPlanDiff` no buffer do prompt.
- **Decisions**: `PlanDiff` carrega `Old` e `New` snapshots completos da fase em `PhaseModification`, não apenas o ID. Custa uma cópia da Phase mas dá ao consumidor JSON tudo que ele precisa para gerar uma visualização própria. O renderer texto usa apenas `Changes` (lista de strings human-readable) para manter a saída tight.
- **Decisions**: `ComputePlanDiff` ordena as listas finais por `Phase.ID` (e `Unchanged` por string) para que o output seja determinístico independente da ordem das fases nos dois planos. Isso facilita golden tests e a diff entre runs.
- **Discovered**: file watcher mid-run **deferido**. fsnotify ainda não está em `go.mod` (o `CLAUDE.md` reserva para Phase 2 da TUI mas a dep nunca foi adicionada). Mesmo com fsnotify, "edição invalida a fase ativa" é semanticamente ambíguo: o agente em modo Director re-edita o spec mid-run para gravar o journal, e essa edição precisa não acionar replanning. A combinação custo-de-design + valor é desfavorável agora; o fluxo `bcc run --director --resume` cobre o caso "usuário editou e reiniciou". Fica como follow-up explícito no plano da fase.
- **Discovered**: `docs/guides/` é diretório novo (não existia no repo). Em vez de `docs/guides/autonomous-execution.md` mencionado pela spec original (que não existe), criei `docs/guides/director.md` (en, canônico) e `docs/guides/director.pt-BR.md` (tradução).

### 2026-05-02 17:30, Phase 8: TUI Director panel and cost reporting

Painel Director cabeado ao TUI: `internal/tui/director.go` define `directorPanel` (plan, status por fase, attempt ativo, latest verdict, custo acumulado) mais `directorKeyMap` e o renderer da modal de escalação. O `Update` consome `PhasePlanned/PhaseBriefed/PhaseReviewed/DirectorEscalation` e atualiza o painel; durante a modal, key presses são desviadas para `handleEscalationKey` que escreve em `escalationGate` e limpa o estado. O `View` insere o box "director" entre `progress` e `risk` quando `directorPanel.active()`, e anexa "director cost: $X.XX" à health box; quando o caminho legado (sem plano) roda, o layout MVP fica intacto. `runDirectorWith` passou a ramificar no `runOutput == OutputTUI`: nova função `runDirectorTUI` em `internal/cli/run_director.go` constrói o programa bubbletea, cria o canal `EscalationReply` que vai tanto para `tui.Options.EscalationGate` quanto para `DirectorPorts.Escalation`, e roda o loop em goroutine. `go test -race ./...` verde com 12 cenários novos em `internal/tui/director_test.go`.

- **Decisions**: `directorPanel.cumulativeCost` soma `KindResultSummary.TotalCostUSD` do executor, não `DirectorCallStats.CostUSD`. A spec pediu "soma de DirectorCallStats.CostUSD por iteração", mas hoje só o executor cost flui como evento; expor o cost do Planner/Briefer/Reviewer exigiria um evento novo (ex: `DirectorCallCompleted{Role, Stats}`), o que estava fora do escopo nominal de P8 ("AgentEventReceived (para custos por iteração)"). O accumulator é estável; quando o evento for adicionado, a mesma chamada `onCost` recebe o valor adicional.
- **Decisions**: `directorPanel` é um campo embutido no `tui.Model`, sem flag `directorEnabled` em `tui.Options`. O render decide via `panel.active()` (plan != nil), que é função do primeiro `PhasePlanned`. Evita um segundo flag de configuração e mantém o mesmo binário rodando MVP ou Director sem ramificação na construção do Model.
- **Decisions**: `tui.Options.EscalationGate` é `chan<- loop.EscalationReply`. O canal real é construído em `runDirectorTUI` com buffer 1 e passado tanto ao Model (write side) quanto ao `DirectorPorts.Escalation` (read side via auto-conversão). Match exato à diretriz da spec ("um único canal carregando `EscalationReply`").
- **Decisions**: nova função `runDirectorTUI` em `internal/cli/run_director.go` em vez de estender `runWithTUI`. O caminho MVP em `runWithTUI` carrega muito comportamento legado (session menu com [r]/[e], `NewEvents` factory, baseline SHA capture) que não se aplica ao Director porque o resume é cabeado pela P9 e o "rebuild loop" não existe neste caminho. Manter as duas funções separadas evita dual-purposing no momento e deixa P9 livre para refazer a session menu.
- **Decisions**: Director events são roteados pelo `Update` regular antes da checagem `m.director.escalation`. Isso permite que `DirectorEscalation` chegue ao painel quando a modal já está visível (e.g., em escalações encadeadas) sem perder o estado anterior. Keys são interceptadas para a modal apenas em `tea.KeyPressMsg`; os eventos seguem o pipeline normal.
- **Decisions**: `withOutputMode(t, mode)` adicionado em `run_director_test.go` para pinar `runOutput` durante os testes. Sem esse override, o caminho TUI tenta abrir `/dev/tty` sob `go test` e falha; com ele, os tests existentes (Happy / AutoProceed) continuam exercitando o caminho `dispatchEvents` (json/text) em vez do bubbletea. Restaura o valor anterior no `t.Cleanup`.
- **Discovered**: a modal de escalação é overlay anexado ao final do dashboard, não substitui o frame. O usuário continua vendo as fases enquanto responde, casando com a UX descrita na PRD ("ver o estado do run e decidir"). Saída da modal é instantânea: a key resolve, `escalationGate` recebe o token, o painel limpa, e o próximo render reflete o novo estado da fase.

### 2026-05-02 17:00, Phase 7: Loop integration and decider rewrite

Multi-fase, retry, e escalação cabeados em `loop.Loop`. Quando `Loop.Director != nil`, `Run` desvia para `runDirector` (em `internal/loop/director_run.go`), que itera pelas fases do plano executando o ciclo brief → execute → review → decide. A cli (`runDirectorWith`) deixou de hospedar o ciclo de uma fase: agora monta o `DirectorPorts`, chama `Loop.Run`, e mapeia o exit code. `errDirectorPipelineNotWired` foi removido junto com `briefExecuteReviewFirstPhase`. Cobertura de loop nova vive em `internal/loop/director_integration_test.go` (7 cenários: approve duplo, revise→approve, escalate+abort, sem gate de escalação, HEAD-stuck, blocked, resume a partir de fase aprovada).

- **Decisions**: `internal/loop/` agora importa `internal/director` para os tipos `Plan/Briefing/Verdict` e as portas `Briefer/Reviewer`. A regra "loop não importa adapters" continua válida porque director é um pacote de domínio peer (não um adapter). A direção oposta segue proibida: `internal/director/director_test.go::TestImports` continua bloqueando imports de `internal/loop` exceto `agentcontract`. Sem essa direção, loop e director ficariam acoplados via boilerplate de conversão de tipos.
- **Decisions**: `DirectorPorts.NewExecutor func(systemPromptFile string) Executor` é uma factory, não um campo `Executor` único. Cada (fase, attempt) instancia um Executor com o caminho do prompt materializado pelo renderer; em testes a closure devolve o mesmo `recordingExecutor` capturando o caminho passado.
- **Decisions**: `EscalationReply` é um `<-chan EscalationReply` em `DirectorPorts.Escalation`, não um callback. A spec pediu reuso do pattern do `PauseGate`, então mantive a forma de canal. nil = "abort em qualquer escalação", para runs headless. O TUI em P8 vai plugar seu próprio canal.
- **Decisions**: `stdinEscalationGate` (cli) é uma goroutine que lê stdin e empurra para o canal sem nunca escrever em stderr. O prompt visível ao usuário sai do evento `DirectorEscalation` consumido pelo render text/json (P8 traz overlay no TUI). Escrever no stderr da gate criaria race com testes que inspecionam o buffer após o loop terminar; render via evento mantém uma única fonte de verdade ("o que o usuário vê é o que o evento descreve").
- **Decisions**: o decider trata `attempt == 1+budget` (último attempt permitido) com `revise` como `Escalate`, e `attempt < 1+budget` com `revise` como `Retry`. budget=2 produz 3 attempts máximo (1, 2, 3). budget=0 escala já no primeiro `revise`. Cobre o cenário "todas as tentativas dentro do retry budget" da PRD.
- **Decisions**: `runDirector` set BCC_ITERATION ao número da tentativa atual da fase (1..1+budget) e `BCC_MAX_ITERATIONS` ao orçamento total da fase (1+budget), em vez do contador global do loop legado. Em modo Director o agente raciocina sobre tentativas-de-fase, não iterações genéricas; o env var reflete isso.
- **Decisions**: `preexistingApprovedPhases` consulta `Store.LatestVerdict` por fase e pula as já aprovadas. É o gancho mínimo para resume; P9 vai cabear `--resume` na cli (esta fase só tem o "skip-if-approved" que cai natural a partir do que já está persistido).
- **Decisions**: signals diferentes de `review`/`done`/`blocked` no caminho Director causam `ExitInvalid` em vez de "skip review". O contrato `markdown_bcc` em modo Director (P6) diz que o agente deve emitir `review` ao final de cada fase; um `continue` ou `unknown` é violação e o loop não tenta interpretar.
- **Discovered**: novos eventos no canal do loop: `PhasePlanned`, `PhaseBriefed`, `PhaseReviewed`, `DirectorEscalation`. `PhaseBriefed` carrega `*director.Briefing` e `PhaseReviewed` carrega `*director.Verdict` (compartilhamento read-only) para o painel TUI de P8 não precisar reler do disco. `eventjson.go` e `eventlevel.go` ganharam casos para os quatro tipos.
- **Discovered**: `Loop.SpecPath` continua sendo a fonte para o spec content em modo Director; `runDirector` relê o disco a cada attempt para alimentar o briefer (com o estado mais recente do journal) e novamente após o executor para computar o journal delta. Isso evita carregar bytes via `DirectorPorts` e mantém o loop consistente quando o agente edita o spec.
- **Discovered**: testes de cli em `run_director_test.go` substituídos pelo conjunto `HappyPath_TwoPhasesApprove` (cobre persistência + run end-to-end), `AbortPath`, `AutoProceedSkipsPrompt`, `RejectsEmptyPlan`. A cobertura de PhaseID rewrite, journal delta, e AcceptanceEvidence migrou para o nível do loop (em `internal/loop/director_integration_test.go`); a cli foca em orchestração + confirmação.

### 2026-05-02 16:30, Phase 6: Review pipeline

Brief→execute→review cabeado para a primeira fase pendente. Após o Executor rodar, bcc captura o sinal mais recente do `iteration_result` via `agentcontract.BccEventIterationResult`, lê HEAD antes/depois, e quando o sinal é `review` ou `done` chama `Reviewer.Review` com `ReviewerInput{Plan, Briefing, Diff, JournalDelta, AcceptanceEvidence}`. O verdict é validado por `ValidateVerdict` e persistido em `.bcc/verdicts/<phase-id>-<attempt>.json`. Multi-fase, retry e escalação seguem para P7; `errDirectorPipelineNotWired` continua pinado, mas pinando agora "P7 wires multi-phase loop".

- **Decisions**: `loop.GitProbe` ganhou o método `Diff(ctx, baseSHA, headSHA)`. A porta já era o probe do working tree e a spec preferia esse caminho a uma porta paralela. O director domain define localmente `director.GitDiffer` (subset com só `Diff`) que `loop.GitProbe` satisfaz estruturalmente; isso preserva a fronteira `internal/director/` ↔ `internal/loop/` enforced por `TestImports` (allowlist só inclui `agentcontract`). `GatherDiff` é uma thin wrapper sobre `GitDiffer.Diff` para manter um único ponto de entrada estável quando P7 cabear o decider.
- **Decisions**: bcc reescreve `Verdict.PhaseID` e `Verdict.Attempt` no lado bcc após o Reviewer responder, espelhando o tratamento de `Plan.SpecHash`/`Plan.PlannedAt` em P4 e `Briefing.PhaseID`/`Briefing.Attempt` em P5. Reviewers adversariais não conseguem corromper a chave de persistência. O teste cobre o caso explicitamente.
- **Decisions**: `AcceptanceEvidence` é passado como `map[string]string{}` (vazio) por bcc. A captura de evidência (saída de `go test`/`go build`) acontece dentro do agente Reviewer, não no lado bcc. Isso mantém bcc lean e respeita o contrato que diz "o Reviewer **roda como agente**, não como bcc-side runner".
- **Decisions**: o canal `agentcontract.SignalUnknown` (sem `iteration_result` emitido) e `SignalContinue`/`SignalBlocked` pulam o Reviewer. Só `SignalReview` e `SignalDone` disparam a auditoria, em linha com a tarefa "rodar Executor → quando `signal == review` ou `signal == done`". Mensagem em stderr explicita a decisão para o usuário.
- **Decisions**: `JournalHeading = "## Execution Journal"` é hard-coded em `internal/director/journal.go` (não importa do markdown_bcc adapter). O director domain segue stdlib-only e a localização do heading é uma preocupação do format adapter; quando outros formatos chegarem, podemos parametrizar via `Plan` ou config. Por ora, casa com a convenção bcc-markdown que esta spec usa.
- **Decisions**: `briefingPromptTemplate` migrou de `const` raw string em `render.go` para `prompts/briefing.md` embarcado via `//go:embed`. Backticks inline no novo bloco "When this phase is complete..." quebrariam o raw string (Go não permite escape em raw strings); a migração casa com o padrão já usado para `plan.md`/`brief.md`/`review.md` e mantém o template editável sem recompilar.
- **Decisions**: `markdown_bcc.Config.DirectorEnabled` adicionado mesmo sem caminho de runtime ativo (em modo Director, o prompt vem de `director/render.go`, não de `markdown_bcc`). A spec pediu o campo explicitamente; o cli boundary o ligará quando/se cabear o adapter markdown_bcc num caminho Director futuro. Hoje só serve de documentação executável.
- **Discovered**: `stubGitProbe` em `internal/cli/run_director_test.go` é um helper local que satisfaz `loop.GitProbe` com SHAs scriptados. Não promove para `internal/git/fake/` porque os outros testes do loop já trazem fakes inline; subir o nível agora seria abstração prematura.
- **Discovered**: `recordingExecutor` ganhou campo opcional `emitSignal agentcontract.Signal` para empurrar um `BccEventIterationResult` ao canal de eventos antes de retornar. Permite exercitar o caminho de review sem subprocesso real e sem subir um fake "executor" novo.

### 2026-05-02 16:00, Phase 5: Briefing pipeline

Brief→execute cabeado para a primeira fase pendente. `BriefingFor` (em `internal/director/briefing.go`) monta um `BrieferInput` com `SpecContent`, lendo veredictos prévios via `Store.ReadVerdict` quando `attempt > 1` e propagando o `Feedback` mais recente como `PriorFeedback`. `RenderBriefingPrompt` (em `internal/director/render.go`) concatena cabeçalho, scope, acceptance, spec excerpt, contexto, `prior_feedback` opcional, e os três partials de `agentcontract` (wire protocol, absolute restrictions, working tree). O executor Claude ganhou `Config.SystemPromptFile`: quando preenchido, passa `--system-prompt-file <path>` e omite o prompt positional. Em `runDirectorWith`, após a confirmação, bcc chama o Briefer, persiste a `Briefing` em `.bcc/briefings/<phase-id>-<attempt>.json`, materializa o prompt em `.prompt.md` ao lado, instancia o Executor com aquele caminho, e roda uma iteração; review e o multi-fase ficam para P6-P7. `errDirectorPipelineNotWired` segue como sentinela final, agora pinando o stub de "review e loop não cabeados".

- **Decisions**: a fronteira em `internal/director/director_test.go` foi relaxada para permitir `internal/loop/agentcontract`. O renderer de Briefing precisa de `agentcontract.Partials()` e o package agentcontract é canônico (peer do director, não um adapter), conforme a referência da spec e o CLAUDE.md. `TestImports` ganhou um allowlist explícito para esse path; tudo mais sob `internal/loop/` continua proibido.
- **Decisions**: `BrieferInput` agora carrega `SpecContent []byte` (campo novo), em vez de o Briefer ler o spec do disco. Mantém o adapter Director livre de I/O sobre o working tree do projeto e dá ao Briefer o spec exato auditado pelo `SpecHash` do plano. O `briefPayload` no adapter Claude e o prompt `brief.md` foram atualizados em conjunto.
- **Decisions**: `directorDeps.newExecutor func(systemPromptFile string) loop.Executor` é uma factory, não um Executor pré-construído. O caminho do prompt só existe depois que o Briefer entrega o Briefing e o renderer escreve o arquivo, então a factory é instanciada por chamada. Em produção a factory captura a config Claude no closure; em testes devolve um `recordingExecutor` que captura o caminho passado.
- **Decisions**: bcc reescreve `Briefing.PhaseID` e `Briefing.Attempt` no lado bcc após o Briefer responder, espelhando o tratamento que `Plan.SpecHash` recebe em P4. Briefers adversariais não conseguem corromper a chave de persistência; a verdade auditável é o que bcc computou.
- **Decisions**: `errDirectorPipelineNotWired` foi mantido como nome (mensagem atualizada) em vez de renomear. O teste de proceed continua pinando o sentinela e a continuação em P6/P7 só precisa apagar o stub. Renomear obrigaria churn em testes que já consumiam o símbolo.
- **Decisions**: `RenderBriefingPrompt` valida que `briefing.PhaseID == phase.ID`; um briefing pareado com a fase errada falha cedo em vez de silenciosamente entregar um prompt mal endereçado ao Executor. Caso de teste cobre o erro.
- **Decisions**: o renderer não importa `text/template` diretamente; usa o `*template.Template` devolvido por `agentcontract.Partials()` e adiciona o template "briefing" via `t.New("briefing").Parse(...)`. Mantém o pacote enxuto e força o reuso dos partials canônicos.
- **Discovered**: golden file em `internal/director/testdata/briefing_golden.md`. `TestRenderBriefingPrompt_Golden` aceita `-update-golden` para regerar quando o template mudar intencionalmente.

### 2026-05-02 15:30, Phase 4: Plan generation and user confirmation

Caminho de entrada do modo Director acoplado em `runDirectorWith`: lê o spec, calcula `SpecHash`, chama `Planner.Plan`, valida via `ValidatePlan`, persiste em `.bcc/plan.json`, renderiza, e bloqueia em uma confirmação `[P]roceed`/`[A]bort`. `--auto-proceed` salta a confirmação. Após o "proceed" a função sai com `errDirectorPipelineNotWired` porque P5-P7 ainda não estão amarrados; o teste pina esse contrato para que uma fase futura remova o stub explicitamente. Renderer de plano vive em `internal/cli/render.go` ao lado do dispatcher de eventos.

- **Decisions**: a função pública `runDirector` chama um `runDirectorWith(... deps, dio)` que recebe `directorDeps{Planner, Store, now}` e `directorIO{stdin, stderr, autoProceed}`. Em produção `runDirector` constrói o adapter Claude e a Store em `.bcc/`; o teste injeta `fake.Planner` e uma Store em `t.TempDir()`. Sem variável global de factory, sem reescrita do call-site em runSpec.
- **Decisions**: bcc reescreve `Plan.SpecHash` e `Plan.PlannedAt` no lado bcc, mesmo se o Planner devolver valores. O hash do conteúdo do disco é a verdade auditável; o `PlannedAt` zerado é preenchido com `deps.now()`. Planners adversariais não conseguem persistir um plano com hash divergente.
- **Decisions**: confirmação aceita `p`/`yes`/`proceed` para proceder e `a`/`no`/`abort` para abortar; entradas inválidas reentram no loop em vez de abortar. EOF em stdin é tratado como abort (fail-closed) para evitar que um TTY ausente em CI green-light um plano não revisado. `--auto-proceed` defaults off pelo mesmo motivo.
- **Decisions**: `[A]bort` deixa `.bcc/plan.json` no disco. O usuário pode inspecionar o JSON depois e decidir se replaneja, edita o spec, ou descarta; rolar o write para trás daria a impressão de que o Planner nunca rodou.
- **Discovered**: adicionada flag `--auto-proceed` ao `runCmd`. Defaults `false`; coberto por teste do flag default (`TestRunCmd_AutoProceedFlagDefaultsOff`).

### 2026-05-02 15:00, Phase 3: Director ports and Claude adapter scaffolding

Portas (`Planner`, `Briefer`, `Reviewer`) declaradas em `internal/director/ports.go`; adapter Claude em `internal/director/claude/` implementa as três, monta a linha de comando `claude -p --bare --no-session-persistence --output-format stream-json --json-schema <tempfile>`, parseia a resposta como o último bloco de texto JSON na stream-json, captura custo/tokens do `result` event, e enforça `MaxBudgetUSD` fail-closed via `ErrBudgetExceeded`. Prompts e schemas embarcam por `//go:embed` no pacote pai (`internal/director/embed.go`); a composição com `agentcontract.Partials()` acontece no adapter, mantendo o pai stdlib-only. Fakes em `internal/director/fake/` substratam P7. `go test -race ./...` verde.

- **Decisions**: schemas começam permissivos onde Go marshalla nil-slices como `null`. `Phase.depends_on`, `scope_in`, `scope_out` aceitam `["array", "null"]` em `plan.schema.json` para que o round-trip Go ↔ schema funcione sem forçar `omitempty` nos tipos (o que afetaria também a persistência em `.bcc/plan.json`). Aperta-se depois se virar fonte de bug.
- **Decisions**: `MaxBudgetUSD` é cap duplo: passamos `--max-budget-usd <n>` ao binário e também checamos `stats.CostUSD > cap` no lado bcc após o `result`. Defesa em profundidade contra drift do binário; o erro é tipado (`ErrBudgetExceeded`) para o decider de P7 escalar a fase.
- **Decisions**: o request payload entra como bloco fenced JSON ("## Request payload") concatenado ao final do prompt-sistema do papel, não via stdin separado. Mantém uma única superfície de entrada (o prompt) e simplifica o adapter. Os payloads são gerados por funções privadas no adapter (`planPayload`/`briefPayload`/`reviewPayload`) porque o contrato com o agente é independente dos tipos de domínio que trocamos com o loop.
- **Decisions**: `github.com/santhosh-tekuri/jsonschema/v6` adicionado ao `go.mod`, mas usado **apenas em testes** do adapter (compilar os schemas embarcados, validar payloads golden). Em runtime, bcc confia na validação server-side do `claude --json-schema` mais o `UnmarshalJSON` dos enums fechados (`VerdictOutcome`, `EvidenceKind`) e os `Validate*` do P1. Sem dep em runtime, sem maintenance burden no caminho quente.
- **Decisions**: o teste de fronteira em `director_test.go` continua válido sem mudança. O sub-pacote `internal/director/claude/` importa `internal/director` e `internal/loop/agentcontract`, ambos legítimos para um adapter; a regra "stdlib-only no pai" é o que `TestImports` enforça e nada novo no pai a violou.

### 2026-05-02 14:30, Phase 2: Director config and CLI flag

Cabeamento opt-in para o caminho Director: tipos `[director]` + `[director.claude]` no `config.Config`, defaults aplicados (`retry_budget=2`, `claude.binary="claude"`), flag `--director` que sobrescreve o TOML, ramificação em `runSpec` para `runDirector` (stub `errors.New("director not yet wired")` até P3-P7), e `bcc init` agora emite as duas subtabelas com cap de custo comentado.

- **Decisions**: o stub `runDirector` mora em arquivo próprio (`internal/cli/run_director.go`), não anexo ao `runSpec`. A ramificação fica no caller (`if cfg.Director.Enabled { return runDirector(...) }`) imediatamente antes do bloco MVP, então P4 só substitui o stub sem mexer no fluxo de validação atual.
- **Decisions**: a flag `--director` mapeia direto em `cfg.Director.Enabled = true`. Sem terceiro estado ou desambiguação por `pflag` tristate; um usuário que quer rodar MVP enquanto tem `[director].enabled = true` pode editar o TOML, é o trade-off aceitável para uma flag binária.
- **Decisions**: as subtabelas `[director]` no `bcc init` ficam descomentadas (`enabled = false`) em vez de comentadas. Mantém o TOML self-documenting: o usuário enxerga a chave como existente e flipa o valor sem grep, e os defaults ainda batem com `ApplyDefaults` se o bloco for removido.

### 2026-05-02 03:15, Wire protocol via MCP tools (cross-cutting fix)

Bug bloqueador surfaceou no run autônomo da P1: o contrato instruía o agente a "emitir JSON line em stdout", mas Claude em `-p --output-format stream-json` envelopa toda saída do agente; nenhum `bcc_event` top-level chegava ao bcc, todas as iterações fechavam com `Signal=Unknown` e `ExitInvalid`. Trocado o transporte do wire por chamadas a tools MCP; o servidor MCP roda in-process no bcc (HTTP loopback com bearer token, `internal/mcp/`), o adapter Claude registra-o via `--mcp-config` + `--strict-mcp-config`, e `parseAssistant` em `internal/executor/claude/` traduz `tool_use` com prefixo `mcp__bcc__` em `KindBccEvent`. Smoke end-to-end (spec mínima de uma fase) confirmado: agente chama `task_started`, faz o trabalho, `task_completed`, `iteration_result` com `value=done`, bcc encerra com `ExitDone`.

- **Decisions**: servidor MCP é in-process (HTTP em `127.0.0.1:0`), não subprocess. Agente conecta via loopback. Token de 32 bytes em `Authorization: Bearer <hex>` impede impersonation por outros processos locais. `--strict-mcp-config` bloqueia MCP servers do user (Notion etc.) durante o run.
- **Decisions**: o servidor MCP é stub: responde `ok` para `tools/call`. O sinal trafega pelo envelope `tool_use` no stream-json, não pela resposta RPC. Mantém um único ponto de extração de eventos (`parseAssistant`), evita sincronização entre dois canais.
- **Decisions**: nomes de tool ficam `task_started`, `task_completed`, `iteration_result` (constantes em `agentcontract.Tool*`). Claude prefixa para `mcp__bcc__<name>` no toolset visível ao agente; o adapter strip-eia o prefixo antes de chamar `agentcontract.FromToolCall`.
- **Decisions**: `BccEvent.Raw map[string]any` substituído por `BccEvent.RawValue string` (apenas o `value` cru de `iteration_result`, para o painel de risk). `ParseLine` removido (era dead code). `BccEventProgressTick` removido (não havia consumidor).
- **Discovered**: `internal/mcp/` (novo pacote stdlib-only, ~250 linhas + testes), `Config.DisableMCP` no executor Claude (escape hatch para fakes que não falam MCP nos testes). `[director.claude].max_budget_usd` da P3 mapeia para `--max-budget-usd` na chamada do Director, mas o **Executor adapter** não passa esse flag; é controle de custo do Director, não do Executor.
- **Problems**: `TestRun_PromptIsLastArg` quebrou ao adicionar `--mcp-config <path-temp> --strict-mcp-config` na ordem dos args → reescrito para isolar o path dinâmico antes de comparar o resto. Testes de fixture (`full-iter.jsonl`, `TestRun_StreamsEventsFromFixture`) atualizados para incluir os três `mcp__bcc__*` tool_use.

### 2026-05-02 14:00, Phase 1: Director domain types and persistence

Domínio + persistência do Director entregues como pacote stdlib-only `internal/director/`. Pronto para P2 cabear o config e P3 montar as portas e o adapter Claude.

- **Decisions**: layer-boundary check em `director_test.go` usa `go/parser` em vez de shell-out a `go list`. A regra fica explícita no código do teste e roda sem dependências externas; `go list` continua aceitável no CI mas não é necessário aqui.
- **Decisions**: `Plan.SpecHash` e `Plan.PlannedAt` adicionados já em P1 (a spec autorizava P1 ou ajuste retroativo em P4); persistir `planned_at` simplifica auditoria sem custo extra.
- **Decisions**: `ValidatePlan` e `ValidateVerdict` implementados junto dos tipos em P1, embora a spec os listasse em P4 e P6. São funções triviais sobre os structs e seu lar natural é `types.go`; P4/P6 só precisam invocá-los.
- **Decisions**: `MarshalJSON` rejeita o zero-value de `VerdictOutcome` e `EvidenceKind` para que um struct meio construído não escape silenciosamente em persistência ou wire.
