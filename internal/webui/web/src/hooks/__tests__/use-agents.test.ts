import { describe, it, expect } from 'vitest'
import { computeAgents, FADE_MS } from '../use-agents'
import type { SeqEvent } from '../use-events'

// Helpers to build events compactly. Wire-level roles always use the
// "bcc-<role>" form the director adapter emits; the executor adapter
// emits the bare "executor" instead. Both are normalized by the reducer.

function spawnStarted(opts: {
  seq: number
  spawnId: string
  role: string
  at: string
  phaseId?: string
  taskId?: string
  iterationId?: string
  model?: string
  effort?: string
  attempt?: number
  promptPath?: string
}): SeqEvent {
  return {
    seq: opts.seq,
    event: {
      type: 'spawn_started',
      at: opts.at,
      spawn_id: opts.spawnId,
      role: opts.role,
      phase_id: opts.phaseId,
      task_id: opts.taskId,
      iteration_id: opts.iterationId,
      model: opts.model,
      effort: opts.effort,
      attempt: opts.attempt,
      prompt_path: opts.promptPath,
    },
  }
}

function spawnFinished(opts: {
  seq: number
  spawnId: string
  role: string
  at: string
  exitCode?: number
  durationMs?: number
  usd?: number
}): SeqEvent {
  return {
    seq: opts.seq,
    event: {
      type: 'spawn_finished',
      at: opts.at,
      spawn_id: opts.spawnId,
      role: opts.role,
      exit_code: opts.exitCode ?? 0,
      duration_ms: opts.durationMs ?? 1000,
      cost: { usd: opts.usd ?? 0 },
    },
  }
}

function agentEv(opts: {
  seq: number
  agentId: string
  role: string
  kind: string
  at: string
  text?: string
  toolId?: string
  toolName?: string
  toolArgs?: Record<string, unknown>
  isError?: boolean
  summary?: string
}): SeqEvent {
  const ev: SeqEvent['event'] & Record<string, unknown> = {
    type: 'agent_event',
    at: opts.at,
    agent_id: opts.agentId,
    role: opts.role,
    kind: opts.kind,
  }
  if (opts.text != null) ev['text'] = opts.text
  if (opts.toolId) {
    if (opts.kind === 'tool_use') {
      ev['tool'] = { id: opts.toolId, name: opts.toolName, args: opts.toolArgs }
    } else {
      ev['tool'] = { id: opts.toolId, is_error: opts.isError ?? false, summary: opts.summary }
    }
  }
  return { seq: opts.seq, event: ev }
}

function iterStarted(seq: number, at: string): SeqEvent {
  return { seq, event: { type: 'iter_started', at, index: 1, max_iter: 5 } }
}

function taskStarted(opts: {
  seq: number
  agentId: string
  phaseId: string
  taskId: string
  at: string
}): SeqEvent {
  return {
    seq: opts.seq,
    event: {
      type: 'task_started',
      at: opts.at,
      agent_id: opts.agentId,
      phase_id: opts.phaseId,
      task_id: opts.taskId,
    },
  }
}

function taskCompleted(opts: {
  seq: number
  agentId: string
  phaseId: string
  taskId: string
  at: string
}): SeqEvent {
  return {
    seq: opts.seq,
    event: {
      type: 'task_completed',
      at: opts.at,
      agent_id: opts.agentId,
      phase_id: opts.phaseId,
      task_id: opts.taskId,
    },
  }
}

// initEv is the first agent_event (kind=init) that follows every spawn_started
// in the wire stream. The reducer uses it to bind agent_id to the most recent
// pending spawn of the same role.
function initEv(seq: number, agentId: string, role: string, at: string): SeqEvent {
  return agentEv({ seq, agentId, role, kind: 'init', at })
}

const T0 = '2026-05-07T12:00:00Z'
const T1 = '2026-05-07T12:00:01Z'
const T2 = '2026-05-07T12:00:02Z'
const T3 = '2026-05-07T12:00:03Z'
const T4 = '2026-05-07T12:00:04Z'
const NOW = Date.parse('2026-05-07T12:00:10Z')

describe('computeAgents', () => {
  it('returns empty state for no events', () => {
    const state = computeAgents([], NOW)
    expect(state.byId).toEqual({})
    expect(state.liveByAnchor.plan).toEqual([])
    expect(state.liveByAnchor.byPhase).toEqual({})
    expect(state.liveByAnchor.byTask).toEqual({})
    expect(state.archivedByAnchor.plan).toEqual([])
  })

  it('binds spawn_started + first agent_event into a live planner card on plan anchor', () => {
    const state = computeAgents(
      [
        spawnStarted({
          seq: 1,
          spawnId: 'sp-planner',
          role: 'bcc-planner',
          at: T0,
          model: 'claude-sonnet-4-6',
          effort: 'medium',
          attempt: 1,
        }),
        initEv(2, 'bcc-planner-aaaa', 'bcc-planner', T0),
      ],
      NOW,
    )
    const planner = state.byId['bcc-planner-aaaa']
    expect(planner).toBeDefined()
    expect(planner.role).toBe('planner')
    expect(planner.status).toBe('live')
    expect(planner.spawnId).toBe('sp-planner')
    expect(planner.anchor).toEqual({ kind: 'plan' })
    expect(planner.model).toBe('claude-sonnet-4-6')
    expect(planner.effort).toBe('medium')
    expect(state.liveByAnchor.plan).toEqual(['bcc-planner-aaaa'])
  })

  it('keeps planner live after spawn_finished until iter_started, then fades, then archives', () => {
    const noIter = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp-p', role: 'bcc-planner', at: T0 }),
        initEv(2, 'bcc-planner-aaaa', 'bcc-planner', T0),
        spawnFinished({ seq: 3, spawnId: 'sp-p', role: 'bcc-planner', at: T1 }),
      ],
      NOW,
    )
    expect(noIter.byId['bcc-planner-aaaa'].status).toBe('live')
    expect(noIter.byId['bcc-planner-aaaa'].fadeAt).toBeUndefined()

    const fading = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp-p', role: 'bcc-planner', at: T0 }),
        initEv(2, 'bcc-planner-aaaa', 'bcc-planner', T0),
        spawnFinished({ seq: 3, spawnId: 'sp-p', role: 'bcc-planner', at: T1 }),
        iterStarted(4, T2),
      ],
      Date.parse(T2) + 100,
    )
    expect(fading.byId['bcc-planner-aaaa'].fadeAt).toBe(T2)
    expect(fading.byId['bcc-planner-aaaa'].status).toBe('fading')

    const archived = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp-p', role: 'bcc-planner', at: T0 }),
        initEv(2, 'bcc-planner-aaaa', 'bcc-planner', T0),
        spawnFinished({ seq: 3, spawnId: 'sp-p', role: 'bcc-planner', at: T1 }),
        iterStarted(4, T2),
      ],
      Date.parse(T2) + FADE_MS + 1000,
    )
    expect(archived.byId['bcc-planner-aaaa'].status).toBe('archived')
    expect(archived.archivedByAnchor.plan).toEqual(['bcc-planner-aaaa'])
    expect(archived.liveByAnchor.plan).toEqual([])
  })

  it('non-planner roles fade immediately on spawn_finished', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp-b', role: 'bcc-briefer', at: T0, phaseId: 'P1' }),
        initEv(2, 'bcc-briefer-bbbb', 'bcc-briefer', T0),
        spawnFinished({ seq: 3, spawnId: 'sp-b', role: 'bcc-briefer', at: T1 }),
      ],
      Date.parse(T1) + 100,
    )
    expect(state.byId['bcc-briefer-bbbb'].status).toBe('fading')
    expect(state.byId['bcc-briefer-bbbb'].fadeAt).toBe(T1)
  })

  it('anchors briefer to its phase', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'bcc-briefer', at: T0, phaseId: 'P1' }),
        initEv(2, 'bcc-briefer-bbbb', 'bcc-briefer', T0),
      ],
      NOW,
    )
    expect(state.byId['bcc-briefer-bbbb'].anchor).toEqual({ kind: 'phase', phaseId: 'P1' })
    expect(state.liveByAnchor.byPhase['P1']).toEqual(['bcc-briefer-bbbb'])
  })

  it('anchors executor to task when task_id present on spawn_started', () => {
    const state = computeAgents(
      [
        spawnStarted({
          seq: 1,
          spawnId: 'sp',
          role: 'executor',
          at: T0,
          phaseId: 'P1',
          taskId: 'T1.1',
        }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].anchor).toEqual({
      kind: 'task',
      phaseId: 'P1',
      taskId: 'T1.1',
    })
    expect(state.liveByAnchor.byTask['P1:T1.1']).toEqual(['bcc-executor-eeee'])
  })

  it('anchors executor to phase when task_id absent on spawn_started', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].anchor).toEqual({ kind: 'phase', phaseId: 'P1' })
  })

  it('anchors reviewer to its task', () => {
    const state = computeAgents(
      [
        spawnStarted({
          seq: 1,
          spawnId: 'sp',
          role: 'bcc-reviewer',
          at: T0,
          phaseId: 'P1',
          taskId: 'T1.1',
        }),
        initEv(2, 'bcc-reviewer-rrrr', 'bcc-reviewer', T0),
      ],
      NOW,
    )
    expect(state.byId['bcc-reviewer-rrrr'].anchor).toEqual({
      kind: 'task',
      phaseId: 'P1',
      taskId: 'T1.1',
    })
  })

  it('captures multi-task in-flight ids on the executor', () => {
    const state = computeAgents(
      [
        spawnStarted({
          seq: 1,
          spawnId: 'sp',
          role: 'executor',
          at: T0,
          phaseId: 'P1',
          taskId: 'T1.1',
        }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        taskStarted({ seq: 3, agentId: 'bcc-executor-eeee', phaseId: 'P1', taskId: 'T1.1', at: T1 }),
        taskStarted({ seq: 4, agentId: 'bcc-executor-eeee', phaseId: 'P1', taskId: 'T1.2', at: T2 }),
        taskCompleted({ seq: 5, agentId: 'bcc-executor-eeee', phaseId: 'P1', taskId: 'T1.1', at: T3 }),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].inFlightTaskIds).toEqual(['T1.2'])
  })

  it('captures latestAssistantText and latestThinking', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'thinking',
          at: T1,
          text: 'pondering...',
        }),
        agentEv({
          seq: 4,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'assistant_text',
          at: T2,
          text: 'I will edit foo.go',
        }),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].latestThinking).toBe('pondering...')
    expect(state.byId['bcc-executor-eeee'].latestAssistantText).toBe('I will edit foo.go')
  })

  it('builds ToolChips for non-Task tool_use, capped at 3', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T1,
          toolId: 'tu-1',
          toolName: 'Read',
          toolArgs: { file_path: '/a/b/foo.go' },
        }),
        agentEv({
          seq: 4,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T2,
          toolId: 'tu-2',
          toolName: 'Edit',
          toolArgs: { file_path: '/a/b/bar.go' },
        }),
        agentEv({
          seq: 5,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T3,
          toolId: 'tu-3',
          toolName: 'Bash',
          toolArgs: { command: 'go test ./...' },
        }),
        agentEv({
          seq: 6,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T4,
          toolId: 'tu-4',
          toolName: 'Read',
          toolArgs: { file_path: '/a/b/baz.go' },
        }),
      ],
      NOW,
    )
    const chips = state.byId['bcc-executor-eeee'].recentTools
    expect(chips).toHaveLength(3)
    expect(chips.map((c) => c.toolUseId)).toEqual(['tu-2', 'tu-3', 'tu-4'])
    expect(chips[0].target).toBe('b/bar.go')
    expect(chips[1].target).toBe('go test ./...')
  })

  it('marks tool chip result on matching tool_result', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T1,
          toolId: 'tu-1',
          toolName: 'Bash',
          toolArgs: { command: 'go test' },
        }),
        agentEv({
          seq: 4,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_result',
          at: T2,
          toolId: 'tu-1',
          isError: true,
        }),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].recentTools[0].result).toBe('error')
  })

  it('models a Task tool call as a sub-agent and closes it on tool_result', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T1,
          toolId: 'tu-task-1',
          toolName: 'Task',
          toolArgs: { subagent_type: 'Explore', prompt: 'Find references to X' },
        }),
        agentEv({
          seq: 4,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_result',
          at: T3,
          toolId: 'tu-task-1',
          isError: false,
          summary: 'Found 5 references',
        }),
      ],
      NOW,
    )
    const subs = state.byId['bcc-executor-eeee'].subAgents
    expect(Object.keys(subs)).toEqual(['tu-task-1'])
    const sub = subs['tu-task-1']
    expect(sub.parentAgentId).toBe('bcc-executor-eeee')
    expect(sub.status).toBe('finished')
    expect(sub.subagentType).toBe('Explore')
    expect(sub.prompt).toBe('Find references to X')
    expect(sub.summary).toBe('Found 5 references')
    expect(state.byId['bcc-executor-eeee'].recentTools).toHaveLength(0)
  })

  it('keeps a sub-agent live until its tool_result arrives', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'tool_use',
          at: T1,
          toolId: 'tu-task-1',
          toolName: 'Task',
          toolArgs: { subagent_type: 'general', prompt: 'investigate' },
        }),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].subAgents['tu-task-1'].status).toBe('live')
    expect(state.byId['bcc-executor-eeee'].subAgents['tu-task-1'].finishedAt).toBeUndefined()
  })

  it('keeps multiple briefers live concurrently for a phase via FIFO pending queue', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp1', role: 'bcc-briefer', at: T0, phaseId: 'P1' }),
        spawnStarted({ seq: 2, spawnId: 'sp2', role: 'bcc-briefer', at: T1, phaseId: 'P1' }),
        initEv(3, 'bcc-briefer-1', 'bcc-briefer', T0),
        initEv(4, 'bcc-briefer-2', 'bcc-briefer', T1),
      ],
      NOW,
    )
    expect(state.liveByAnchor.byPhase['P1']).toEqual(['bcc-briefer-1', 'bcc-briefer-2'])
    expect(state.byId['bcc-briefer-1'].spawnId).toBe('sp1')
    expect(state.byId['bcc-briefer-2'].spawnId).toBe('sp2')
  })

  it('places older reviewers in archive after fade window', () => {
    const state = computeAgents(
      [
        spawnStarted({
          seq: 1,
          spawnId: 'sp1',
          role: 'bcc-reviewer',
          at: T0,
          phaseId: 'P1',
          taskId: 'T1.1',
          attempt: 1,
        }),
        initEv(2, 'bcc-reviewer-1', 'bcc-reviewer', T0),
        spawnFinished({ seq: 3, spawnId: 'sp1', role: 'bcc-reviewer', at: T1 }),
        spawnStarted({
          seq: 4,
          spawnId: 'sp2',
          role: 'bcc-reviewer',
          at: T2,
          phaseId: 'P1',
          taskId: 'T1.1',
          attempt: 2,
        }),
        initEv(5, 'bcc-reviewer-2', 'bcc-reviewer', T2),
        spawnFinished({ seq: 6, spawnId: 'sp2', role: 'bcc-reviewer', at: T3 }),
        spawnStarted({
          seq: 7,
          spawnId: 'sp3',
          role: 'bcc-reviewer',
          at: T4,
          phaseId: 'P1',
          taskId: 'T1.1',
          attempt: 3,
        }),
        initEv(8, 'bcc-reviewer-3', 'bcc-reviewer', T4),
      ],
      Date.parse(T3) + FADE_MS + 100,
    )
    expect(state.archivedByAnchor.byTask['P1:T1.1']).toEqual([
      'bcc-reviewer-1',
      'bcc-reviewer-2',
    ])
    expect(state.liveByAnchor.byTask['P1:T1.1']).toEqual(['bcc-reviewer-3'])
  })

  it('stores cost and duration from spawn_finished', () => {
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        spawnFinished({
          seq: 3,
          spawnId: 'sp',
          role: 'executor',
          at: T1,
          exitCode: 0,
          durationMs: 1234,
          usd: 0.42,
        }),
      ],
      Date.parse(T1) + 10,
    )
    expect(state.byId['bcc-executor-eeee'].exitCode).toBe(0)
    expect(state.byId['bcc-executor-eeee'].durationMs).toBe(1234)
    expect(state.byId['bcc-executor-eeee'].costUSD).toBe(0.42)
  })

  it('synthesizes an agent card even when spawn_started is missing (lossy old session)', () => {
    const state = computeAgents(
      [
        agentEv({
          seq: 1,
          agentId: 'bcc-executor-orphan',
          role: 'bcc-executor',
          kind: 'init',
          at: T0,
        }),
        agentEv({
          seq: 2,
          agentId: 'bcc-executor-orphan',
          role: 'bcc-executor',
          kind: 'assistant_text',
          at: T1,
          text: 'hello',
        }),
      ],
      NOW,
    )
    const card = state.byId['bcc-executor-orphan']
    expect(card).toBeDefined()
    expect(card.role).toBe('executor')
    expect(card.spawnId).toBeUndefined()
    expect(card.latestAssistantText).toBe('hello')
  })

  it('truncates very long assistant_text and thinking to MAX_TEXT_LEN', () => {
    const long = 'x'.repeat(2000)
    const state = computeAgents(
      [
        spawnStarted({ seq: 1, spawnId: 'sp', role: 'executor', at: T0, phaseId: 'P1', taskId: 'T1.1' }),
        initEv(2, 'bcc-executor-eeee', 'bcc-executor', T0),
        agentEv({
          seq: 3,
          agentId: 'bcc-executor-eeee',
          role: 'bcc-executor',
          kind: 'assistant_text',
          at: T1,
          text: long,
        }),
      ],
      NOW,
    )
    expect(state.byId['bcc-executor-eeee'].latestAssistantText!.length).toBe(800)
  })
})
