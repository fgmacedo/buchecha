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

- [ ] G1: Pacote `internal/director/` com tipos canônicos (`Plan`, `Phase`, `AcceptanceItem`, `Briefing`, `Verdict`, `VerdictFeedback`), serialização JSON e persistência em `.bcc/{plan,briefings,verdicts}/`.
- [ ] G2: Adapter Director Claude (`internal/director/claude/`) implementando `Planner`, `Briefer` e `Reviewer`, todas instrumentadas com cost reporting.
- [ ] G3: `bcc run --director <spec>` que planeja, confirma com o usuário, e executa o ciclo brief/execute/review até `done`.
- [ ] G4: Decider rescrito para tratar verdicts do Director como autoritativos; retry budget configurável por fase com escalação.
- [ ] G5: Painel TUI de estado do Director (fase ativa, briefing resumido, verdict, custo).
- [ ] G6: `bcc run --resume` reconstrói estado a partir de `.bcc/{plan,briefings,verdicts}/` sem replanejar quando o hash do spec é o mesmo.

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
enabled = false                     # opt-in; CLI --director liga sem editar TOML
retry_budget = 2                    # default por fase; phase.retry_budget no plan sobrescreve

[director.claude]
binary = "claude"                   # default: PATH lookup
model = ""                          # vazio = default do binário
extra_args = []
max_budget_usd = 0                  # 0 = sem cap; > 0 vira --max-budget-usd
```

### Wire protocol additions

Nenhuma mudança no `bcc_event` do Executor. O Director comunica com o bcc por **stdout JSON validado contra schema** (uma chamada `claude --json-schema <arquivo>` por papel). Não há sentinelas adicionais sobre stdin do Director nem sobre stdout do Executor.

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

1. [ ] Criar `internal/director/director.go` (apenas comentário de pacote descrevendo o propósito e regras de fronteira) e `internal/director/types.go` com structs `Plan`, `Phase`, `AcceptanceItem`, `Briefing`, `Verdict`, `VerdictFeedback`, `RequiredChange`, `OutOfScopeNote`. Campos exatamente como na schema ilustrativa da PRD; `executor_assignment` permanece como `*ExecutorAssignment` opcional para a PRD 4.
1. [ ] Definir tipos enumerados como strings: `VerdictOutcome` (`approve`/`revise`/`escalate`), `EvidenceKind` (`diff`/`test`/`build`/`manual`); cada um com `String()` e `MarshalJSON`/`UnmarshalJSON` validando o conjunto fechado. Round-trip golden test cobrindo cada valor.
1. [ ] Criar `internal/director/ids.go` com `func PhaseID(specHash string, intent string) string` retornando `sha256(specHash + "\x00" + intent)` em hex truncado para 16 caracteres. Teste de estabilidade e teste de colisão entre (hash, intent) diferentes.
1. [ ] Criar `internal/director/store.go` com `Store` struct sobre `string baseDir` (e.g. `.bcc/`) expondo `WritePlan(*Plan)`, `ReadPlan() (*Plan, error)`, `WriteBriefing(*Briefing)`, `ReadBriefing(phaseID string, attempt int)`, `WriteVerdict(*Verdict)`, `ReadVerdict(...)`, `LatestVerdict(phaseID string)`. Cada `Write*` cria dirs faltantes (`MkdirAll 0o755`); todo `Read*` retorna `fs.ErrNotExist` envelopado quando ausente.
1. [ ] Adicionar função `SpecHash(content []byte) string` em `internal/director/ids.go`: sha256 hex completo. Caso de teste: dois bytes idênticos produzem o mesmo hash; bytes com BOM diferem.
1. [ ] Tabela de testes em `store_test.go`: round-trip com `t.TempDir()`, leitura de arquivo ausente, leitura de JSON corrompido (deve retornar erro envelopado, nunca panicar), `LatestVerdict` percorrendo `attempt` 1..N e retornando o maior.
1. [ ] Adicionar verificação de fronteira: `internal/director/director.go` lista no comentário os pacotes proibidos; um teste em `director_test.go` (placeholder) garante via `go list -deps ./internal/director` que nenhum desses imports apareça (executado pelo CI; pode ser inicialmente um teste shell-out com `go list`).

### Phase 2: Director config and CLI flag

**Context:** Antes de cabear adapters, o usuário precisa de um caminho ergonômico para ligar o Director: `--director` no CLI ou `[director].enabled = true` no `.bcc.toml`. Esta fase também adiciona `[director.claude]` com `max_budget_usd` e `retry_budget` global. Nenhuma execução real ainda; o cabeamento só decide se o caminho Director é tomado mais à frente.

**Depends on:** P1 (referencia `director.RetryBudget` se for definido como tipo; evite acoplar, prefira `int` puro neste momento).

**Acceptance:**

1. `bcc run --director <spec>` aceita a flag e propaga até a função de execução; `bcc run` sem ela mantém o comportamento MVP idêntico, validado por `internal/cli/run_test.go`.
2. `[director]` e `[director.claude]` são parseados pelo loader TOML existente sem mudar o decoder.
3. `bcc run --director` em uma config sem `[director]` aplica defaults (`retry_budget=2`, `max_budget_usd=0`).

**Tasks:**

1. [ ] Adicionar `Director DirectorConfig` em `internal/config/config.go` com `Enabled bool`, `RetryBudget int`, `Claude DirectorClaude`. `DirectorClaude` carrega `Binary string`, `Model string`, `ExtraArgs []string`, `MaxBudgetUSD float64`.
1. [ ] Atualizar `internal/config/defaults.go` para preencher `RetryBudget=2` e `Claude.Binary="claude"` quando ausentes. Teste em `defaults_test.go`.
1. [ ] Adicionar flag `--director` ao `runCmd` em `internal/cli/run.go`. Quando `--director` é passado, `cfg.Director.Enabled = true` (override sobre TOML).
1. [ ] No início de `runSpec`, ramificar: se `cfg.Director.Enabled`, tomar o caminho `runDirector(...)` (stub que retorna `errors.New("director not yet wired")` nesta fase); senão, manter o caminho atual. Teste verifica a ramificação via flag.
1. [ ] Atualizar `bcc init` (`internal/cli/init.go`) para escrever `[director]` e `[director.claude]` com defaults comentados. Não interativo nesta fase; segunda volta no wizard fica fora de escopo.
1. [ ] Atualizar `internal/configloader/toml/toml_test.go` com fixture cobrindo `[director]` e `[director.claude]`.

### Phase 3: Director ports and Claude adapter scaffolding

**Context:** Define o contrato programático que o restante das fases consome. As portas vivem no consumidor (`internal/director/ports.go`); o adapter `internal/director/claude/` implementa cada porta delegando ao binário `claude` em modo `-p --bare --no-session-persistence --json-schema <arquivo>`. Esta fase entrega Planner/Briefer/Reviewer como interfaces, o adapter Claude inicial cobrindo as três operações com testes baseados em fakes (não invocamos o binário real ainda; isso vem em P4-P6 nos testes de integração).

**Depends on:** P1.

**Acceptance:**

1. `var _ director.Planner = (*director_claude.Adapter)(nil)` (e idem para `Briefer`, `Reviewer`) compila.
2. Testes unitários em `internal/director/claude/claude_test.go` exercitam: montagem de argumentos da CLI; parse e validação do payload de retorno; falha quando JSON é inválido; falha quando tokens excedem o cap configurado.
3. Os três prompts (`plan.md`, `brief.md`, `review.md`) e os três schemas (`*.schema.json`) embarcam via `//go:embed` e estão acessíveis pelo adapter.

**Tasks:**

1. [ ] Criar `internal/director/ports.go` com:
   - `type Planner interface { Plan(ctx context.Context, in PlannerInput) (*Plan, *DirectorCallStats, error) }`
   - `type Briefer interface { Brief(ctx context.Context, in BrieferInput) (*Briefing, *DirectorCallStats, error) }`
   - `type Reviewer interface { Review(ctx context.Context, in ReviewerInput) (*Verdict, *DirectorCallStats, error) }`
   - `DirectorCallStats { DurationMS int64; CostUSD float64; InputTokens, OutputTokens int64 }` para cost reporting (NFR3).
1. [ ] Definir `PlannerInput`, `BrieferInput`, `ReviewerInput`: structs com somente os dados que cada papel precisa. Planner recebe `SpecPath`, `SpecContent []byte`, `SpecHash string`. Briefer recebe `*Plan`, `phaseID string`, `attempt int`, `priorVerdicts []*Verdict`, `priorFeedback *VerdictFeedback`. Reviewer recebe `*Plan`, `*Briefing`, `diff string`, `journalDelta string`, `acceptanceEvidence map[string]string`.
1. [ ] Criar `internal/director/prompts/{plan,brief,review}.md` com o prompt operacional do papel, instruindo (a) ler somente as entradas do payload (sem leitura de filesystem além do `SpecPath` quando aplicável), (b) emitir um único objeto JSON conforme o schema, (c) seguir as `absolute_restrictions` (compostas via partial; reusa `agentcontract.Partials()`).
1. [ ] Criar `internal/director/schemas/{plan,briefing,verdict}.schema.json` (JSON Schema 2020-12) modelando os tipos da P1. Validar via `go test` rodando `jsonschema.Compile(...)` sobre os arquivos (dependência: `github.com/santhosh-tekuri/jsonschema/v6`, novo go.mod entry; alternativa stdlib-only: validação por unmarshal estrito + verificações manuais. Preferir a dep externa por economia de manutenção; só ela na lista de deps novas).
1. [ ] Criar `internal/director/claude/claude.go` com `Adapter` struct exportando `New(cfg Config) *Adapter`. `Config` carrega `Binary`, `Model`, `ExtraArgs`, `MaxBudgetUSD`, `Stderr io.Writer`, `CancelGrace time.Duration`.
1. [ ] Implementar método interno `runJSONCall(ctx, schemaPath string, prompt string) ([]byte, *DirectorCallStats, error)`: monta `claude -p --bare --no-session-persistence --output-format stream-json --json-schema <path> [--model <m>] [--max-budget-usd <n>] <prompt>`, lê stdout linha a linha, captura o último `result` para extrair custo/tokens (reaproveita `parseResult` mental do `internal/executor/claude/claude.go`), encontra o objeto JSON do schema na sequência stream-json, retorna bytes + stats. Cancelamento espelha `executor/claude/claude.go` (SIGINT + WaitDelay).
1. [ ] Implementar `Plan`, `Brief`, `Review` chamando `runJSONCall` com o prompt e schema apropriados; serializar o `*Input` em um payload JSON inline anexado ao prompt; deserializar a resposta para o tipo de domínio; retornar.
1. [ ] Adicionar fakes em `internal/director/fake/fake.go`: `FakePlanner`, `FakeBriefer`, `FakeReviewer` com slots `PlanFn`, `BriefFn`, `ReviewFn` (funções que o teste escreve). Isso será o substrato para o teste de loop em P7.

### Phase 4: Plan generation and user confirmation

**Context:** Ponto de entrada do modo Director. Gera o plano, persiste, mostra ao usuário e espera confirmação antes do primeiro brief. Re-planejamento com diff fica em P9.

**Depends on:** P2, P3.

**Acceptance:**

1. `bcc run --director <spec>` lê o spec, chama `Planner.Plan`, persiste em `.bcc/plan.json`, e bloqueia esperando `[P]roceed`/`[A]bort` no stdin (ou `--auto-proceed` aprova sem bloquear). `[A]bort` retorna `ExitInvalid` sem rodar o Executor.
2. Plano com zero fases é rejeitado com erro claro (`director: planner returned an empty plan`); ExitInvalid.
3. Persistência inclui spec_hash; rerun com spec idêntico e `--resume` (P9) reutiliza; sem `--resume`, replaneja.

**Tasks:**

1. [ ] Criar `internal/cli/run_director.go` com `func runDirector(ctx, cancel, specPath, cfg)` (chamado a partir do branch da flag em P2).
1. [ ] Em `runDirector`: ler spec content, calcular `SpecHash`, instanciar `Adapter` Director Claude com `cfg.Director.Claude`, instanciar `Store` em `.bcc/`, chamar `Planner.Plan`, persistir.
1. [ ] Renderer de plano: nova função em `internal/cli/render.go` que imprime `Plan` em texto (heading, success_criteria, tabela de fases). Em modo TUI o painel terá variante visual em P8; aqui texto na stderr é suficiente para a confirmação.
1. [ ] Bloco de confirmação: lê uma linha de `os.Stdin`; aceita `p`/`P`, `a`/`A`. Flag `--auto-proceed` salta. Em modo TUI (`--output tui`), o input vem por uma `tea.Cmd` no Model (cabeamento mínimo nesta fase: sair do TUI durante a confirmação, igual ao padrão `[e]` no `runWithTUI`; refinamento em P8).
1. [ ] Validação de plano: se `len(plan.Phases) == 0`, abortar; se houver `phase.depends_on` referenciando IDs ausentes, abortar. Função pura `ValidatePlan(*Plan) error` em `internal/director/types.go`, com testes.
1. [ ] Spec hash calculado a partir do conteúdo lido do disco; persiste no `Plan.SpecHash` (campo novo no struct, adicionado em P1 se ainda não estiver ou em ajuste retroativo aqui com migração de schema).
1. [ ] Teste de integração em `internal/cli/run_director_test.go` usando `internal/director/fake.FakePlanner` para devolver um plano scriptado; verifica que `.bcc/plan.json` é criado e a confirmação `p\n` desbloqueia.

### Phase 5: Briefing pipeline

**Context:** Para cada fase pendente do plano, o Briefer produz um `Briefing`; o briefing é renderizado em um arquivo system-prompt e o Executor é invocado com `--system-prompt-file`. O loop ainda não tem o ciclo brief/execute/review fechado, foco aqui é entregar uma fase ao Executor com o briefing certo e capturar a saída.

**Depends on:** P3, P4.

**Acceptance:**

1. Dado um plano e uma fase pendente, `Briefer.Brief` produz `Briefing` que é persistido em `.bcc/briefings/<phase-id>-1.json` e materializado em `.bcc/briefings/<phase-id>-1.prompt.md`.
2. O prompt materializado contém o spec excerpt pré-computado, scope, acceptance, e o `prior_feedback` quando attempt > 1; partials de `agentcontract` (wire protocol, absolute restrictions, working tree) ficam concatenados, jamais omitidos.
3. O Executor existente (`internal/executor/claude/`) aceita o caminho do prompt e roda; chamada de teste em `internal/cli/run_director_test.go` verifica que o subprocesso é chamado com `--system-prompt-file <path>` (em vez do prompt inline atual).

**Tasks:**

1. [ ] Adicionar `BriefingFor(plan *Plan, phaseID string, attempt int) (*BrieferInput, error)` em `internal/director/`, montando o input com priors (lê `Store.LatestVerdict` para attempt > 1).
1. [ ] Criar `internal/director/render.go` com `func RenderBriefingPrompt(*Briefing, *Phase) (string, error)`: usa `text/template` + `agentcontract.Partials()` para concatenar (a) cabeçalho com fase/attempt/scope, (b) acceptance criteria com IDs, (c) `prior_feedback` (renderizado em prosa: lista de `required_changes` e `out_of_scope`), (d) os três partials (wire_protocol, absolute_restrictions, working_tree).
1. [ ] Estender `internal/executor/claude/claude.go` para aceitar `Config.SystemPromptFile string`; quando preenchido, o adapter passa `--system-prompt-file <path>` e omite o prompt positional. Default zero mantém o comportamento atual. Teste em `claude_test.go`.
1. [ ] Em `runDirector`, fechar um ciclo "uma fase só" (placeholder até P7): para a primeira fase pendente, chamar `Briefer.Brief`, persistir, renderizar prompt em `.bcc/briefings/<phase-id>-<attempt>.prompt.md`, instanciar `Executor` com `SystemPromptFile=<path>`, rodar uma iteração. Sem review ainda (P6/P7).
1. [ ] Spec excerpt: `BrieferInput` carrega o spec inteiro mas o Briefer recebe instrução de incluir somente o trecho relevante em `Briefing.SpecExcerpt`. Não fazemos slicing no bcc-side.
1. [ ] Teste de golden: `RenderBriefingPrompt` contra um `*Briefing` fixo produz uma string estável; arquivo em `internal/director/testdata/briefing_golden.md`.

### Phase 6: Review pipeline

**Context:** Após o Executor sair com `iteration_result=review` (a PRD redefine quando emitir `review`: ao terminar uma fase do plano, e não apenas em gates pelo observador), o Director roda o Reviewer. O Reviewer reúne evidências por critério de aceite (diff, test, build, manual), produz `Verdict`, persiste.

**Depends on:** P3, P5.

**Acceptance:**

1. Dado um diff de teste e um `Briefing` correspondente, `Reviewer.Review` retorna um `Verdict` válido (passes pelo schema) e bcc persiste em `.bcc/verdicts/<phase-id>-<attempt>.json`.
2. Para cada `AcceptanceItem.Evidence == "test"` ou `"build"`, o Reviewer **deve** consultar a evidência: o adapter Director Claude executa o agente passando o diff + uma instrução de rodar `go test` / `go build` localmente; o resultado entra em `acceptance_results[].note`. Para `manual`, o `note` é "manual; deferred to user via reasoning".
3. Verdict com `outcome=revise` carrega `feedback` não vazio; `outcome=approve` exige todos os `acceptance_results.passed=true`; `outcome=escalate` carrega `reasoning` com a razão. Validação ao parse, com erro claro.

**Tasks:**

1. [ ] `func GatherDiff(ctx, git GitProbe, baseSHA, headSHA string) (string, error)` no `internal/director/`: porta nova `GitDiff` definida no consumidor (ou método extra em `loop.GitProbe`; preferir o último porque a porta já é o probe do working tree). Implementa em `internal/git/cli/cli.go`.
1. [ ] `func GatherJournalDelta(specBefore, specAfter []byte) string`: extrai diferença textual da seção `## Execution Journal`. Pure function; teste com spec antes/depois.
1. [ ] `Reviewer.Review` chamado com `ReviewerInput{Plan, Briefing, Diff, JournalDelta, AcceptanceEvidence}`. AcceptanceEvidence é um `map[string]string` (key = `AcceptanceItem.ID`, value = log/output capturado para evidências `test`/`build`); o adapter Claude obtém isso instruindo o próprio agent (o Reviewer **roda como agente**, não como bcc-side runner; bcc só lhe entrega o diff e o briefing e confia que ele executa).
1. [ ] `ValidateVerdict(*Verdict) error`: verifica os invariantes (approve sem failed; revise com feedback; escalate com reasoning). Em `types.go`, com tabela de testes.
1. [ ] Em `runDirector` (placeholder de P5), substituir o "uma fase só" por: rodar Executor → quando `signal == review` ou `signal == done`, gather diff + journal delta + chamar Reviewer → persistir verdict. Sem retry/escalate ainda.
1. [ ] Atualizar `internal/format/markdown_bcc/contract.md`: na seção "Procedure" do modo loop, em modo Director o agente **deve emitir `iteration_result=review`** ao final de cada fase (em vez de `continue`). A escolha de modo é injetada via novo campo `templateData.DirectorEnabled bool`. Esta é a única mudança no contrato; a alternativa é não tocar e tratar `signal=continue` como "fase em progresso", mas o decider fica simples se review marca fim-de-fase.

### Phase 7: Loop integration and decider rewrite

**Context:** Junta tudo: state machine `brief → execute → review → decide` com retry budget, escalação, e tratamento de spec done apenas quando todas as fases têm verdict approve. O decider existente (`internal/loop/decider.go`) é estendido (não substituído) com um `DirectorDecider` que recebe `Verdict` e retorna `Action`. O `Loop.Run` atual ramifica em modo Director.

**Depends on:** P5, P6.

**Acceptance:**

1. `bcc run --director <spec>` em um spec com 3 fases mock (via fakes Director e Executor) executa as 3 fases sequencialmente e termina com `ExitDone`.
2. Quando o Reviewer fake retorna `revise` na primeira tentativa de uma fase, a fase é re-briefada (attempt=2) com `prior_feedback`; segundo attempt aprova; loop continua.
3. Quando o Reviewer fake retorna `revise` em todas as tentativas dentro do retry budget, o loop pausa em `DirectorEscalation` e espera input. Em texto/JSON mode, o input vem de stdin; em TUI vem do painel (cabeamento em P8).
4. `go test -race ./...` verde com integration test cobrindo aprovar / revise+approve / escalate / abort fluxos.

**Tasks:**

1. [ ] Criar `internal/loop/director_decider.go` com `DirectorDecide(in DirectorDeciderInput) DirectorDecision`:
   - Input: `Verdict`, `attempt`, `retryBudget`, `headAdvanced bool`.
   - Output: `DirectorAction` (`AdvancePhase`, `RetryPhase`, `Escalate`, `Abort`, `CompleteSpec`) + `ExitCode` quando aplicável.
   - HEAD-stuck primeiro (espelha o decider atual): se Executor não commitou, sempre abort com `ExitHEADStuck`, independente do verdict.
   - Tabela de teste exaustiva (verdict outcome × attempt × budget × headAdvanced). 12 a 20 casos.
1. [ ] Estender `loop.Loop` com campo opcional `Director DirectorPorts` (struct: `Planner`, `Briefer`, `Reviewer`, `Store`). Quando `Director != nil`, `Run` toma o caminho Director. Caminho legado preservado.
1. [ ] Implementar o caminho Director em `loop.Loop.Run`:
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
1. [ ] Reusar `PauseGate` (já existe em `loop.Loop`) para a escalação: a gate é canalizada do TUI; em modo texto/JSON, um `os.Stdin` reader em goroutine resolve via `[r]esume`/`[s]kip`/`[a]bort`.
1. [ ] Emitir `PhasePlanned`, `PhaseBriefed`, `PhaseReviewed`, `DirectorEscalation` no canal `events` para que renders consumam.
1. [ ] Integration test em `internal/loop/director_integration_test.go` usando fakes (todos: `director.FakePlanner/Briefer/Reviewer`, `loop.fakeExecutor`, `loop.fakeGitProbe`); cobre os fluxos da acceptance.

### Phase 8: TUI Director panel and cost reporting

**Context:** O Director é invisível no MVP TUI atual. Esta fase cria um painel dedicado mostrando estado vivo: plano (com checkboxes derivados de verdicts), fase ativa, briefing resumido (intent + scope + acceptance), verdict mais recente, custo acumulado por papel. Cobre G5 e parcialmente NFR3 (cost reporting na UI).

**Depends on:** P7.

**Acceptance:**

1. Em `bcc run --director <spec>` com `--output tui`, um novo painel "Director" mostra: phases `[x]/[/]/[ ]/[!]` (approved/in-progress/pending/escalated), fase ativa em destaque, custo acumulado.
2. Painel "Health" do TUI atual ganha uma linha "director cost: $X.XX" com soma de `DirectorCallStats.CostUSD` por iteração.
3. Em escalação, um overlay modal mostra `verdict.reasoning` e os botões `[R]esume`, `[S]kip`, `[A]bort`. Input do usuário é roteado para o `PauseGate` correto.

**Tasks:**

1. [ ] Criar `internal/tui/director.go` com Model do painel: estado `plan *Plan`, `currentPhaseID string`, `latestVerdict *Verdict`, `cumulativeCost float64`. Renderer em lipgloss respeitando `--no-color`.
1. [ ] Cabear painel ao Update existente: novo case para `PhasePlanned`, `PhaseBriefed`, `PhaseReviewed`, `DirectorEscalation`, `AgentEventReceived` (para custos por iteração).
1. [ ] Layout: na grade atual, alocar slot para o painel Director quando o modo está ligado (Layout decide a partir de uma flag no Options). Quando desligado, layout MVP intacto.
1. [ ] Modal de escalação: bubbletea state que captura todas as keys até o usuário escolher; `R`/`S`/`A` enviam ao `PauseGate` o token semântico correspondente. Estende `tui.Gate` com canais separados ou um único canal carregando `EscalationReply` (preferir o último).
1. [ ] Atualizar `runWithTUI` em `internal/cli/run.go` para construir o Model com o `*Plan` carregado e os canais Director.
1. [ ] Snapshot tests em `director_test.go` com cenários: plano fresco, fase em progresso, fase escalada, plano todo aprovado.

### Phase 9: Resume, spec-change replan, and documentation

**Context:** Polimento que fecha os requisitos NFR5 (resume) e FR4 (re-plan on spec change). Inclui documentação operacional e atualização do índice da iniciativa.

**Depends on:** P7, P8.

**Acceptance:**

1. `bcc run --director --resume <spec>` em uma sessão interrompida reconstrói plano + verdicts existentes e retoma da próxima fase pendente. Se o `SpecHash` divergir do persistido, replaneja e mostra `[D]iff [P]roceed [A]bort` ao usuário.
2. Editar o spec mid-run cancela a sessão Executor em curso (se a edição invalida a fase ativa), pede confirmação do novo plano, e reinicia a fase afetada.
3. `docs/specs/director/index.md` atualizado: PRD 2 marcada como tendo spec de implementação, link para esta spec.

**Tasks:**

1. [ ] Adicionar flag `--resume` ao `runCmd` (já existe em outras formas; verificar e reusar). Em `runDirector`, se `--resume`: ler `.bcc/plan.json`, comparar `Plan.SpecHash` com `SpecHash(currentSpecBytes)`; igual = continuar; diferente = `RePlanFlow`.
1. [ ] `RePlanFlow`: chamar Planner novamente; calcular diff (`PlanDiff(old, new) PlanDiff` puro: phases adicionadas, removidas, modificadas); renderizar; pedir confirmação. Cancela Executor em curso via `cancel()` do contexto e dispara `LoopFinished{user cancelled}` da iteração morta antes de retomar.
1. [ ] File watcher mid-run: opcional; se simples, usar `fsnotify` (já no `go.mod`) para observar o `SpecPath`. Quando muda durante uma fase, emitir `SpecChanged`; o loop trata na próxima volta. Se complexo, deixar para follow-up e documentar.
1. [ ] `PlanDiff` e renderer em `internal/director/diff.go`; tabela de testes. Suporte a saída texto + JSON.
1. [ ] Atualizar `docs/specs/director/index.md` adicionando referência a esta spec na tabela "Documents in this initiative".
1. [ ] Atualizar `CLAUDE.md` se necessário com a nota: `internal/director/` é domínio puro; adapters em `internal/director/<adapter>/`. (Apenas se a regra atual sobre `internal/loop/` precisar generalizar; preferir não tocar `CLAUDE.md` se possível.)
1. [ ] Adicionar `docs/guides/director.md` curto: o que `--director` faz, como interpretar verdicts, como retomar, como configurar `[director]`. Em pt e en (seguindo convenção do `autonomous-execution.md`).

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

(empty until first execution)
