import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CostMeter } from '../index'
import type { CostAgg } from '../../../hooks/use-cost-aggregator'

// makeAgg builds a CostAgg fixture with the 5-bucket vendor-neutral
// totalTokens shape; tests pass the buckets they care about and inherit
// zeros for the rest. totalTokensSum is precomputed so the headline
// matches what the SPA would derive in production.
function makeAgg(opts: {
  totalUSD?: number
  totalTokens?: Partial<CostAgg['totalTokens']>
  perRole?: CostAgg['perRole']
  perIteration?: CostAgg['perIteration']
}): CostAgg {
  const totalTokens: CostAgg['totalTokens'] = {
    input_fresh: 0,
    input_cached: 0,
    cache_write: 0,
    output: 0,
    reasoning: 0,
    ...(opts.totalTokens ?? {}),
  }
  const totalTokensSum =
    totalTokens.input_fresh +
    totalTokens.input_cached +
    totalTokens.cache_write +
    totalTokens.output +
    totalTokens.reasoning
  return {
    totalUSD: opts.totalUSD ?? 0,
    totalTokens,
    totalTokensSum,
    perRole: opts.perRole ?? {},
    perIteration: opts.perIteration ?? [],
  }
}

describe('CostMeter', () => {
  it('renders USD amount to two decimals', () => {
    const agg = makeAgg({
      totalUSD: 1.23456,
      totalTokens: { input_fresh: 100, output: 50, input_cached: 10, cache_write: 5 },
      perRole: { executor: { usd: 1.23456, tokens: 165 } },
      perIteration: [{ iterationIndex: 0, usd: 1.23456, tokens: 165 }],
    })

    render(<CostMeter agg={agg} />)
    const usdDisplay = screen.getByText('$1.23')
    expect(usdDisplay).toBeInTheDocument()
  })

  it('renders zero USD when no cost data', () => {
    const agg = makeAgg({})

    render(<CostMeter agg={agg} />)
    const usdDisplay = screen.getByText('$0.00')
    expect(usdDisplay).toBeInTheDocument()
  })

  it('renders total tokens count summing all five buckets', () => {
    const agg = makeAgg({
      totalUSD: 0.1,
      totalTokens: { input_fresh: 100, output: 50, input_cached: 10, cache_write: 5 },
    })

    render(<CostMeter agg={agg} />)
    // 100 + 10 + 5 + 50 + 0 = 165
    const tokenDisplay = screen.getByText('165')
    expect(tokenDisplay).toBeInTheDocument()
  })

  it('opens popover on button click', () => {
    const agg = makeAgg({
      totalUSD: 0.1,
      totalTokens: { input_fresh: 100, output: 50 },
      perRole: { executor: { usd: 0.1, tokens: 150 } },
    })

    render(<CostMeter agg={agg} />)
    const button = screen.getByTitle('Cost breakdown')
    fireEvent.click(button)

    // The popover surfaces both the new By Bucket section and the existing By Role section.
    expect(screen.getByText('By Bucket')).toBeInTheDocument()
    expect(screen.getByText('By Role')).toBeInTheDocument()
  })

  it('renders per-bucket breakdown in popover', () => {
    const agg = makeAgg({
      totalUSD: 0.28,
      totalTokens: {
        input_fresh: 210,
        output: 105,
        input_cached: 15,
        cache_write: 7,
      },
      perRole: {
        planner: { usd: 0.05, tokens: 75 },
        executor: { usd: 0.15, tokens: 165 },
        reviewer: { usd: 0.08, tokens: 97 },
      },
    })

    render(<CostMeter agg={agg} />)
    fireEvent.click(screen.getByTitle('Cost breakdown'))

    // Each of the five buckets renders a row; values sit alongside their label.
    expect(screen.getByTestId('bucket-fresh')).toHaveTextContent('210')
    expect(screen.getByTestId('bucket-cached')).toHaveTextContent('15')
    expect(screen.getByTestId('bucket-cache-write')).toHaveTextContent('7')
    expect(screen.getByTestId('bucket-output')).toHaveTextContent('105')
    expect(screen.getByTestId('bucket-reasoning')).toHaveTextContent('0')
  })

  it('renders per-role breakdown in popover', () => {
    const agg = makeAgg({
      totalUSD: 0.28,
      totalTokens: {
        input_fresh: 210,
        output: 105,
        input_cached: 15,
        cache_write: 7,
      },
      perRole: {
        planner: { usd: 0.05, tokens: 75 },
        executor: { usd: 0.15, tokens: 165 },
        reviewer: { usd: 0.08, tokens: 97 },
      },
    })

    render(<CostMeter agg={agg} />)
    fireEvent.click(screen.getByTitle('Cost breakdown'))

    expect(screen.getByText('planner')).toBeInTheDocument()
    expect(screen.getByText('executor')).toBeInTheDocument()
    expect(screen.getByText('reviewer')).toBeInTheDocument()
  })

  it('renders per-iteration breakdown in popover', () => {
    const agg = makeAgg({
      totalUSD: 0.31,
      totalTokens: { input_fresh: 300, output: 150 },
      perIteration: [
        { iterationIndex: 0, usd: 0.1, tokens: 150 },
        { iterationIndex: 1, usd: 0.09, tokens: 120 },
        { iterationIndex: 2, usd: 0.12, tokens: 180 },
      ],
    })

    render(<CostMeter agg={agg} />)
    fireEvent.click(screen.getByTitle('Cost breakdown'))

    expect(screen.getByText('By Iteration')).toBeInTheDocument()
    expect(screen.getByText('Iter 0')).toBeInTheDocument()
    expect(screen.getByText('Iter 1')).toBeInTheDocument()
    expect(screen.getByText('Iter 2')).toBeInTheDocument()
  })

  it('closes popover on escape key', () => {
    const agg = makeAgg({
      totalUSD: 0.1,
      totalTokens: { input_fresh: 100, output: 50 },
      perRole: { executor: { usd: 0.1, tokens: 150 } },
    })

    render(<CostMeter agg={agg} />)
    fireEvent.click(screen.getByTitle('Cost breakdown'))

    const bucketHeader = screen.getByText('By Bucket')
    expect(bucketHeader).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(bucketHeader).not.toBeInTheDocument()
  })
})
