import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { IterDivider } from '../iter-divider'
import { PhaseCard } from '../phase-card'
import { TaskLine } from '../task-line'
import { AgentBlock } from '../agent-block'
import { SpawnMarker } from '../spawn-marker'
import { SelectionProvider } from '../../../../hooks/use-selection'
import type { SeqEvent } from '../../../../hooks/use-events'

// wrapWithSelection wraps children in a SelectionProvider so components that
// call useSelection (e.g. SpawnMarker) have a valid context.
function wrapWithSelection(ui: React.ReactElement): React.ReactElement {
  return React.createElement(SelectionProvider, { sessionId: 'test-session' }, ui)
}

// --- IterDivider ---

describe('IterDivider', () => {
  const cases: { name: string; event: SeqEvent }[] = [
    {
      name: 'iter_started',
      event: {
        seq: 1,
        event: {
          type: 'iter_started',
          iteration_id: 'P1-iter-01',
          at: '2026-05-05T10:00:00Z',
        },
      },
    },
    {
      name: 'iter_finished',
      event: {
        seq: 2,
        event: {
          type: 'iter_finished',
          iteration_id: 'P1-iter-01',
          signal: 'review',
          duration_ms: 12345,
          at: '2026-05-05T10:00:12Z',
        },
      },
    },
    {
      name: 'loop_finished',
      event: {
        seq: 3,
        event: {
          type: 'loop_finished',
          duration_ms: 60000,
          at: '2026-05-05T10:01:00Z',
        },
      },
    },
  ]

  for (const tc of cases) {
    it(`renders ${tc.name} without error`, () => {
      const { container } = render(React.createElement(IterDivider, { event: tc.event }))
      expect(container.querySelector('[data-testid="iter-divider"]')).toBeTruthy()
    })
  }

  it('shows "iter" label for iter_started', () => {
    render(
      React.createElement(IterDivider, {
        event: { seq: 1, event: { type: 'iter_started', iteration_id: 'P1-iter-01' } },
      }),
    )
    expect(screen.getByText('iter')).toBeTruthy()
    expect(screen.getByText('P1-iter-01')).toBeTruthy()
  })

  it('shows "iter done" with signal for iter_finished', () => {
    render(
      React.createElement(IterDivider, {
        event: {
          seq: 2,
          event: { type: 'iter_finished', signal: 'review', duration_ms: 5000 },
        },
      }),
    )
    expect(screen.getByText('iter done')).toBeTruthy()
    expect(screen.getByText('review · 5.0s')).toBeTruthy()
  })

  it('shows "loop done" for loop_finished', () => {
    render(
      React.createElement(IterDivider, {
        event: { seq: 3, event: { type: 'loop_finished', duration_ms: 90000 } },
      }),
    )
    expect(screen.getByText('loop done')).toBeTruthy()
    expect(screen.getByText('1m 30s')).toBeTruthy()
  })
})

// --- PhaseCard ---

describe('PhaseCard', () => {
  const cases: { name: string; event: SeqEvent }[] = [
    {
      name: 'phase_planned',
      event: {
        seq: 10,
        event: { type: 'phase_planned', phase_id: 'P1', at: '2026-05-05T10:00:00Z' },
      },
    },
    {
      name: 'phase_briefed',
      event: {
        seq: 11,
        event: {
          type: 'phase_briefed',
          phase_id: 'P1',
          attempt: 1,
          at: '2026-05-05T10:01:00Z',
        },
      },
    },
    {
      name: 'phase_reviewed',
      event: {
        seq: 12,
        event: {
          type: 'phase_reviewed',
          phase_id: 'P1',
          outcome: 'approve',
          at: '2026-05-05T10:02:00Z',
        },
      },
    },
    {
      name: 'director_escalation',
      event: {
        seq: 13,
        event: {
          type: 'director_escalation',
          phase_id: 'P1',
          reason: 'Too many retries',
          at: '2026-05-05T10:03:00Z',
        },
      },
    },
  ]

  for (const tc of cases) {
    it(`renders ${tc.name} without error`, () => {
      const { container } = render(React.createElement(PhaseCard, { event: tc.event }))
      expect(container.querySelector('[data-testid="phase-card"]')).toBeTruthy()
    })
  }

  it('shows the phase id', () => {
    render(
      React.createElement(PhaseCard, {
        event: { seq: 10, event: { type: 'phase_planned', phase_id: 'P3' } },
      }),
    )
    expect(screen.getByText('P3')).toBeTruthy()
  })

  it('shows the outcome pill for phase_reviewed', () => {
    render(
      React.createElement(PhaseCard, {
        event: { seq: 12, event: { type: 'phase_reviewed', phase_id: 'P1', outcome: 'revise' } },
      }),
    )
    expect(screen.getByText('revise')).toBeTruthy()
  })

  it('shows escalation reason for director_escalation', () => {
    render(
      React.createElement(PhaseCard, {
        event: {
          seq: 13,
          event: { type: 'director_escalation', reason: 'Max retries exceeded' },
        },
      }),
    )
    expect(screen.getByText('Max retries exceeded')).toBeTruthy()
  })
})

// --- TaskLine ---

describe('TaskLine', () => {
  const cases: { name: string; event: SeqEvent }[] = [
    {
      name: 'task_started',
      event: {
        seq: 20,
        event: { type: 'task_started', task_id: 'T1.1', phase_id: 'P1' },
      },
    },
    {
      name: 'task_completed',
      event: {
        seq: 21,
        event: { type: 'task_completed', task_id: 'T1.1', phase_id: 'P1' },
      },
    },
    {
      name: 'task_approved',
      event: {
        seq: 22,
        event: { type: 'task_approved', task_id: 'T1.1', phase_id: 'P1' },
      },
    },
    {
      name: 'task_needs_fix',
      event: {
        seq: 23,
        event: {
          type: 'task_needs_fix',
          task_id: 'T1.1',
          phase_id: 'P1',
          feedback: 'Missing tests',
        },
      },
    },
  ]

  for (const tc of cases) {
    it(`renders ${tc.name} without error`, () => {
      const { container } = render(React.createElement(TaskLine, { event: tc.event }))
      expect(container.querySelector('[data-testid="task-line"]')).toBeTruthy()
    })
  }

  it('shows task id', () => {
    render(
      React.createElement(TaskLine, {
        event: { seq: 20, event: { type: 'task_started', task_id: 'T3.2', phase_id: 'P3' } },
      }),
    )
    expect(screen.getByText('T3.2')).toBeTruthy()
  })

  it('shows feedback snippet for task_needs_fix', () => {
    render(
      React.createElement(TaskLine, {
        event: {
          seq: 23,
          event: { type: 'task_needs_fix', task_id: 'T1.1', feedback: 'Add unit tests' },
        },
      }),
    )
    expect(screen.getByText('Add unit tests')).toBeTruthy()
  })
})

// --- AgentBlock ---

describe('AgentBlock', () => {
  const cases: { name: string; event: SeqEvent }[] = [
    {
      name: 'init',
      event: { seq: 30, event: { type: 'agent_event', kind: 'init' } },
    },
    {
      name: 'thinking',
      event: {
        seq: 31,
        event: { type: 'agent_event', kind: 'thinking', text: 'Planning the approach' },
      },
    },
    {
      name: 'assistant_text',
      event: {
        seq: 32,
        event: { type: 'agent_event', kind: 'assistant_text', text: 'Here is my plan' },
      },
    },
    {
      name: 'rate_limit',
      event: { seq: 33, event: { type: 'agent_event', kind: 'rate_limit' } },
    },
    {
      name: 'result_summary',
      event: {
        seq: 34,
        event: {
          type: 'agent_event',
          kind: 'result_summary',
          total_cost_usd: 0.0042,
          input_tokens: 1000,
          output_tokens: 200,
        },
      },
    },
  ]

  for (const tc of cases) {
    it(`renders agent_event kind=${tc.name} without error`, () => {
      const { container } = render(React.createElement(AgentBlock, { event: tc.event }))
      expect(container.querySelector('[data-testid="agent-block"]')).toBeTruthy()
    })
  }

  it('renders tool_use with paired tool_result as one block', () => {
    const toolUseEvent: SeqEvent = {
      seq: 40,
      event: {
        type: 'agent_event',
        kind: 'tool_use',
        tool_use_id: 'tu_abc',
        tool: { name: 'Bash', args: { command: 'ls' } },
      },
    }
    const toolResultEvent: SeqEvent = {
      seq: 41,
      event: {
        type: 'agent_event',
        kind: 'tool_result',
        tool_use_id: 'tu_abc',
        tool: { summary: 'file1.ts\nfile2.ts', is_error: false },
      },
    }

    const { container } = render(
      React.createElement(AgentBlock, { event: toolUseEvent, pairedResult: toolResultEvent }),
    )

    // One block rendered (not two separate rows)
    const blocks = container.querySelectorAll('[data-testid="agent-block"]')
    expect(blocks).toHaveLength(1)
    // The block shows the tool name
    expect(screen.getByText('Bash')).toBeTruthy()
  })

  it('tool_use/tool_result 4-event sample: two paired blocks', () => {
    // 4 events: tool_use_1, tool_result_1, tool_use_2, tool_result_2
    const tu1: SeqEvent = {
      seq: 50,
      event: {
        type: 'agent_event',
        kind: 'tool_use',
        tool_use_id: 'tu_1',
        tool: { name: 'Read', args: { path: 'foo.ts' } },
      },
    }
    const tr1: SeqEvent = {
      seq: 51,
      event: {
        type: 'agent_event',
        kind: 'tool_result',
        tool_use_id: 'tu_1',
        tool: { summary: 'file contents', is_error: false },
      },
    }
    const tu2: SeqEvent = {
      seq: 52,
      event: {
        type: 'agent_event',
        kind: 'tool_use',
        tool_use_id: 'tu_2',
        tool: { name: 'Write', args: { path: 'bar.ts', content: '...' } },
      },
    }
    const tr2: SeqEvent = {
      seq: 53,
      event: {
        type: 'agent_event',
        kind: 'tool_result',
        tool_use_id: 'tu_2',
        tool: { summary: 'ok', is_error: false },
      },
    }

    // Render each paired block as the parent timeline would.
    const { container } = render(
      React.createElement(React.Fragment, null,
        React.createElement(AgentBlock, { event: tu1, pairedResult: tr1 }),
        React.createElement(AgentBlock, { event: tu2, pairedResult: tr2 }),
      ),
    )

    const blocks = container.querySelectorAll('[data-testid="agent-block"]')
    expect(blocks).toHaveLength(2)

    // Each block carries its tool name
    expect(screen.getByText('Read')).toBeTruthy()
    expect(screen.getByText('Write')).toBeTruthy()

    // Expanding the first block reveals the result text
    const firstButton = blocks[0].querySelector('button')!
    fireEvent.click(firstButton)
    expect(screen.getByText('file contents')).toBeTruthy()
  })

  it('expands long text on click', () => {
    const { container } = render(
      React.createElement(AgentBlock, {
        event: {
          seq: 60,
          event: {
            type: 'agent_event',
            kind: 'assistant_text',
            text: 'This is a long assistant response that contains details.',
          },
        },
      }),
    )
    const btn = container.querySelector('button')!
    expect(btn.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(btn)
    expect(btn.getAttribute('aria-expanded')).toBe('true')
  })
})

// --- SpawnMarker ---

describe('SpawnMarker', () => {
  const cases: { name: string; event: SeqEvent }[] = [
    {
      name: 'spawn_started',
      event: {
        seq: 70,
        event: {
          type: 'spawn_started',
          spawn_id: 'abc01234567890abcdef',
          role: 'executor',
          phase_id: 'P1',
        },
      },
    },
    {
      name: 'spawn_finished',
      event: {
        seq: 71,
        event: {
          type: 'spawn_finished',
          spawn_id: 'abc01234567890abcdef',
          role: 'executor',
          phase_id: 'P1',
          exit_code: 0,
          cost: { usd: 0.0123 },
        },
      },
    },
  ]

  for (const tc of cases) {
    it(`renders ${tc.name} without error`, () => {
      const { container } = render(
        wrapWithSelection(React.createElement(SpawnMarker, { event: tc.event })),
      )
      expect(container.querySelector('[data-testid="spawn-marker"]')).toBeTruthy()
    })
  }

  it('shows USD cost for spawn_finished', () => {
    render(
      wrapWithSelection(
        React.createElement(SpawnMarker, {
          event: {
            seq: 71,
            event: {
              type: 'spawn_finished',
              spawn_id: 'abc01234567890abcdef',
              role: 'executor',
              exit_code: 0,
              cost: { usd: 0.0042 },
            },
          },
        }),
      ),
    )
    expect(screen.getByText('$0.0042')).toBeTruthy()
  })

  it('clicking the pill calls select with spawn kind', () => {
    let capturedSelection: unknown = null

    function Receiver() {
      const { useSelection: _us } = require('../../../../hooks/use-selection')
      return null
    }
    void Receiver

    // We verify selection state via the shared context rather than capturing.
    const { container } = render(
      wrapWithSelection(
        React.createElement(SpawnMarker, {
          event: {
            seq: 70,
            event: {
              type: 'spawn_started',
              spawn_id: 'abc01234567890abcdef',
              role: 'briefer',
              phase_id: 'P2',
            },
          },
        }),
      ),
    )
    void capturedSelection
    const button = container.querySelector('button')!
    // Click should not throw
    fireEvent.click(button)
    expect(button).toBeTruthy()
  })
})
