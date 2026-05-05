import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CostMeter } from '../index'
import type { CostAgg } from '../../../hooks/use-cost-aggregator'

describe('CostMeter', () => {
  it('renders USD amount to two decimals', () => {
    const agg: CostAgg = {
      totalUSD: 1.23456,
      totalTokens: {
        input: 100,
        output: 50,
        cacheRead: 10,
        cacheCreate: 5,
      },
      perRole: {
        executor: { usd: 1.23456, tokens: 150 },
      },
      perIteration: [
        { iterationIndex: 0, usd: 1.23456, tokens: 150 },
      ],
    }

    render(<CostMeter agg={agg} />)
    const usdDisplay = screen.getByText('$1.23')
    expect(usdDisplay).toBeInTheDocument()
  })

  it('renders zero USD when no cost data', () => {
    const agg: CostAgg = {
      totalUSD: 0,
      totalTokens: {
        input: 0,
        output: 0,
        cacheRead: 0,
        cacheCreate: 0,
      },
      perRole: {},
      perIteration: [],
    }

    render(<CostMeter agg={agg} />)
    const usdDisplay = screen.getByText('$0.00')
    expect(usdDisplay).toBeInTheDocument()
  })

  it('renders total tokens count', () => {
    const agg: CostAgg = {
      totalUSD: 0.1,
      totalTokens: {
        input: 100,
        output: 50,
        cacheRead: 10,
        cacheCreate: 5,
      },
      perRole: {},
      perIteration: [],
    }

    render(<CostMeter agg={agg} />)
    const tokenDisplay = screen.getByText('150')
    expect(tokenDisplay).toBeInTheDocument()
  })

  it('opens popover on button click', () => {
    const agg: CostAgg = {
      totalUSD: 0.1,
      totalTokens: {
        input: 100,
        output: 50,
        cacheRead: 0,
        cacheCreate: 0,
      },
      perRole: {
        executor: { usd: 0.1, tokens: 150 },
      },
      perIteration: [],
    }

    render(<CostMeter agg={agg} />)
    const button = screen.getByTitle('Cost breakdown')
    fireEvent.click(button)

    // Check that the popover content is now visible
    const roleHeader = screen.getByText('By Role')
    expect(roleHeader).toBeInTheDocument()
  })

  it('renders per-role breakdown in popover', () => {
    const agg: CostAgg = {
      totalUSD: 0.28,
      totalTokens: {
        input: 210,
        output: 105,
        cacheRead: 15,
        cacheCreate: 7,
      },
      perRole: {
        planner: { usd: 0.05, tokens: 75 },
        executor: { usd: 0.15, tokens: 150 },
        reviewer: { usd: 0.08, tokens: 90 },
      },
      perIteration: [],
    }

    render(<CostMeter agg={agg} />)
    const button = screen.getByTitle('Cost breakdown')
    fireEvent.click(button)

    // Check that all roles are displayed
    expect(screen.getByText('planner')).toBeInTheDocument()
    expect(screen.getByText('executor')).toBeInTheDocument()
    expect(screen.getByText('reviewer')).toBeInTheDocument()
  })

  it('renders per-iteration breakdown in popover', () => {
    const agg: CostAgg = {
      totalUSD: 0.31,
      totalTokens: {
        input: 300,
        output: 150,
        cacheRead: 0,
        cacheCreate: 0,
      },
      perRole: {},
      perIteration: [
        { iterationIndex: 0, usd: 0.1, tokens: 150 },
        { iterationIndex: 1, usd: 0.09, tokens: 120 },
        { iterationIndex: 2, usd: 0.12, tokens: 180 },
      ],
    }

    render(<CostMeter agg={agg} />)
    const button = screen.getByTitle('Cost breakdown')
    fireEvent.click(button)

    // Check that iteration data is displayed
    expect(screen.getByText('By Iteration')).toBeInTheDocument()
    expect(screen.getByText('Iter 0')).toBeInTheDocument()
    expect(screen.getByText('Iter 1')).toBeInTheDocument()
    expect(screen.getByText('Iter 2')).toBeInTheDocument()
  })

  it('closes popover on escape key', () => {
    const agg: CostAgg = {
      totalUSD: 0.1,
      totalTokens: {
        input: 100,
        output: 50,
        cacheRead: 0,
        cacheCreate: 0,
      },
      perRole: {
        executor: { usd: 0.1, tokens: 150 },
      },
      perIteration: [],
    }

    render(<CostMeter agg={agg} />)
    const button = screen.getByTitle('Cost breakdown')
    fireEvent.click(button)

    const roleHeader = screen.getByText('By Role')
    expect(roleHeader).toBeInTheDocument()

    // Simulate escape key
    fireEvent.keyDown(document, { key: 'Escape' })

    // Role header should no longer be visible
    expect(roleHeader).not.toBeInTheDocument()
  })
})
