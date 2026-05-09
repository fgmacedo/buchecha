# Director: um loop de planejamento e revisão sobre o bcc

O Director é o modo padrão do `bcc run`: uma sessão que planeja, briefa, executa e revisa contra o seu spec. São quatro papéis cognitivos, cada um spawnado como um agente Claude Code separado e conectado ao bcc por um único servidor MCP:

- **Planner**: lê o spec e emite um Plano tipado (um DAG de fases e tasks).
- **Briefer**: escolhe a próxima sub-DAG elegível e emite um Briefing por iteração.
- **Executor**: executa o trabalho briefado e reporta progresso por task.
- **Reviewer**: audita o diff do Executor contra os critérios de aceite por task e decide approve, revise ou escalate.

bcc é o orquestrador: ele dona o loop, o estado por sessão, o servidor MCP, a TUI e o protocolo entre papéis.

O design completo está na [PRD 5](../specs/director/2026-05-02-executable-plan-dag.md); este guia é a referência de operação.

## Como rodar

```bash
bcc run docs/specs/<spec>.md
```

Director é o único modo. O loop legado de agente único não é mais exposto pela CLI.

```toml
# .bcc.toml
[director]
retry_budget = 2     # tentativas padrão por sub-DAG antes de escalar
mcp_audit = true     # grava cada chamada MCP em <session-dir>/mcp-log.jsonl

[director.claude]
binary = "claude"
# model = "claude-opus-4-7"
extra_args = []
max_budget_usd = 0   # > 0 limita cada chamada do Director; falha fechada quando excede
```

## O que acontece em uma execução

```
spec → Planner ─► Plano (DAG de fases e tasks)
          │
          ▼
   enquanto há tasks pendentes:
     Briefer ─► Briefing (uma fase, uma sub-DAG)
        │
        ▼
     para attempt em 1..1+retry_budget:
       Executor ─► progresso por task, iteration_finished
       Reviewer ─► verdict por task, review_finished
                       │
                       ▼
              approve | revise | escalate
```

1. O Planner lê o spec via Read tool e submete o Plano por `plan_emit`. bcc valida a estrutura (ids de fase únicos, ids de task únicos por fase, sem ciclos, sem deps cross-phase) e persiste no diretório da sessão.
2. Enquanto houver task `pending` ou `needs_fix`, o Briefer escolhe uma fase elegível, decide qual subconjunto de suas tasks tentar nesta iteração (a **sub-DAG**) e submete o Briefing por `briefing_emit`.
3. O Executor lê o Briefing por `get_briefing`, executa o trabalho e reporta progresso por task com `task_started` / `task_completed`. Fecha com `iteration_finished(signal, summary)`.
4. O Reviewer lê o Briefing, a baseline da fase (`get_baseline`) e o delta do journal (`get_journal_delta`); usa Bash com git diff/log/show para inspecionar o trabalho cumulativo; audita cada task; chama `task_approved` ou `task_needs_fix(feedback)`; e fecha com `review_finished(outcome, reasoning)`.
5. O decider percorre o estado da sub-DAG: toda task `done` avança a iteração; qualquer `needs_fix` re-roda o Executor com o feedback por task incorporado no próximo prompt; um `escalate` explícito (ou esgotar o retry budget) pausa o loop e pergunta ao usuário.

`bcc run` retorna `ExitDone` somente quando o DAG não tem mais tasks pendentes.

## Sessões

Cada invocação `bcc run` é uma **sessão** com um id estável de 12 caracteres hex. Todo artefato gerado pela run vive sob o diretório da sessão; sessões nunca se sobrescrevem entre si.

```
.bcc/
└── sessions/
    └── <session-id>/
        ├── manifest.json           Session{ID, SpecPath, SpecHash, CreatedAt, UpdatedAt, Status}
        ├── plan.json               Plano canônico emitido pelo Planner
        ├── dag.json                estado vivo do DAG (status por task)
        ├── briefings/<iter-id>.json         Briefing como emitido
        ├── briefings/<iter-id>.prompt.md    system prompt materializado do Executor
        └── mcp-log.jsonl           log append-only de cada chamada MCP
```

`Status` cicla entre `running` (início), `escalated_pending` (loop pausado esperando resposta humana), `done` (saída limpa) e `aborted` (qualquer saída diferente de `done`). O manifest é reescrito de forma atômica em cada mudança.

### Listar e inspecionar

```bash
bcc sessions list             # ordenadas pela última atualização
bcc sessions list --output json
bcc sessions show <id>        # manifest completo em texto
bcc sessions show <id> --output json
```

### Semântica de retomada

```bash
bcc run --resume docs/specs/<spec>.md                     # sessão mais recente para esse spec
bcc run --resume --session <id> docs/specs/<spec>.md      # sessão específica
bcc run --session <id> docs/specs/<spec>.md               # sessão específica, sem fallback
```

Cenários:

1. **`--resume` sozinho**: bcc procura sessões cujo `spec_path` corresponda e retoma a mais recente. Spec hash inalterado reusa o Plano persistido. Spec hash divergente dispara o Planner, imprime um `PlanDiff` para registro, persiste o novo Plano e arranca o loop. Sem sessão correspondente, bcc cria uma sessão nova e segue.
2. **`--resume --session <id>`**: igual ao caso anterior, mas a sessão nomeada é a retomada. Spec divergente devolve `ErrSessionSpecMismatch`; id ausente devolve `ErrSessionNotFound`.
3. **`--session <id>` sem `--resume`**: a sessão nomeada é reaberta; bcc nunca cria uma sessão nova nessa forma. Id ausente é fatal.
4. **Sem flags**: uma sessão nova é criada.

Quando uma sessão retomada tem tasks travadas em `in_progress` (um agente que morreu mid-iteration), bcc as reescreve para `pending` para que a próxima iteração as pegue.

## Comunicação MCP

Toda mensagem entre bcc e um agente é uma chamada MCP roteada pelo servidor MCP run-wide do bcc. A superfície completa por papel está em [`internal/loop/agentcontract/wire_protocol.md`](../../internal/loop/agentcontract/wire_protocol.md). Visão geral:

| Papel | Lê | Escreve |
|---|---|---|
| Planner | spec via Read tool | `plan_emit`, `task_started/completed("planning")` |
| Briefer | `get_dag_snapshot` | `briefing_emit` |
| Executor | `get_briefing`, `get_pending_tasks` | `task_started/completed`, `iteration_finished` |
| Reviewer | `get_briefing`, `get_baseline`, `get_journal_delta`, `get_dag_snapshot` | `task_approved`, `task_needs_fix(feedback)`, `review_finished` |

Toda chamada carrega o `agent_id` que bcc embutiu no prompt do papel. Chamadas sem `agent_id`, com id não registrado ou com papel que não bate com a connection, são rejeitadas com erro estruturado.

## Escalação de quatro opções

Quando o Reviewer retorna `escalate`, ou quando o retry budget esgota em `revise`, bcc pausa o loop e pergunta ao usuário. As opções:

| Tecla | Resposta | Efeito |
|---|---|---|
| `R` | resume com hint | O próximo Briefing para a sub-DAG ainda pendente recebe o hint como bloco "User hint (escalation)" acima do feedback do Reviewer. |
| `F` | force-approve | bcc marca sinteticamente todas as tasks da sub-DAG ainda pendentes como `done`. O audit log registra a escrita sintética sob `role: "user"` e `method: "bcc_force_approve"`. |
| `S` | skip | A fase fica como está e o run termina com `ExitInvalid` no fim. |
| `A` | abort | O run para imediatamente. |

No modo TUI a modal abre na tela de escolha e, em `R`, troca para um input de hint onde Enter submete e Esc cancela de volta. Nos modos text/json o gate stdin lê a letra e, em `r`, lê a próxima linha como o hint (linha vazia significa sem hint).

## Troubleshooting

Quando uma run dá errado, o audit log é o primeiro lugar para olhar.

```bash
tail -n 50 .bcc/sessions/<id>/mcp-log.jsonl
```

Cada linha é `{at, role, agent_id, method, input, result, err?}`. Padrões comuns:

- **Planner ficou rejeitando `plan_emit`**: o erro do validador está em `err`; geralmente ciclo de fase, fase vazia ou id duplicado.
- **Briefer emitiu sub-DAG vazia**: aparece como erro de `briefing_emit` com `empty sub_dag_task_ids`. A fase elegível não tinha tasks `pending` nem `needs_fix`; cheque `dag.json`.
- **Reviewer nunca chamou `review_finished`**: o loop trata isso como `escalate`. O audit log mostra o método terminal ausente; o Reviewer provavelmente saiu cedo.
- **Executor não fez avançar o HEAD**: o loop termina com `head_stuck`. O Executor não produziu commits durante a tentativa; geralmente falha de tooling ou prompt que não comitou.

Desligue o audit log com `[director].mcp_audit = false` se ele crescer demais; o formato é JSONL puro mas uma run longa pode produzir centenas de kilobytes.

## Limites hoje

- O Director roda apenas contra o adapter Claude. O protocolo MCP é vendor-neutral por construção; adapters codex e gemini estão desbloqueados mas não foram escritos.
- Edições no spec mid-run não são detectadas automaticamente. Edite o spec, pare o run, e rode `bcc run --resume <spec>` para pegar a mudança.
- Atribuição capability-aware (modelo por task) está rastreada na [issue #3](https://github.com/fgmacedo/buchecha/issues/3); o Plano ainda não carrega metadados de executor por task.
- Execução paralela de sub-DAGs em worktrees está rastreada na [issue #2](https://github.com/fgmacedo/buchecha/issues/2); hoje o loop roda um Executor por vez.
