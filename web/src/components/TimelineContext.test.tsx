import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TimelineProvider, useTimelineContext } from './TimelineContext'
import { ReactNode } from 'react'

function TestConsumer({ children }: { children: (ctx: ReturnType<typeof useTimelineContext>) => ReactNode }) {
  const ctx = useTimelineContext()
  return <>{children(ctx)}</>
}

function createMockT(key: string) {
  return key
}

describe('TimelineContext', () => {
  const defaultValues = {
    expandedId: null,
    setExpandedId: () => {},
    contentVersionId: null,
    setContentVersionId: () => {},
    contentMap: new Map(),
    loadingContent: null,
    selectedIds: [],
    filePath: 'src/main.ts',
    project: 'test',
    t: createMockT,
    summaries: new Map(),
  }

  it('provides context values to children', () => {
    render(
      <TimelineProvider value={defaultValues}>
        <TestConsumer>
          {(ctx) => <div data-testid="result">{ctx.filePath} - {ctx.project}</div>}
        </TestConsumer>
      </TimelineProvider>
    )
    expect(screen.getByTestId('result').textContent).toBe('src/main.ts - test')
  })

  it('provides selectedIds from context', () => {
    render(
      <TimelineProvider value={{ ...defaultValues, selectedIds: [1, 2] }}>
        <TestConsumer>
          {(ctx) => <div data-testid="result">{ctx.selectedIds.join(',')}</div>}
        </TestConsumer>
      </TimelineProvider>
    )
    expect(screen.getByTestId('result').textContent).toBe('1,2')
  })

  it('provides contentMap from context', () => {
    const contentMap = new Map([[1, 'content-1'], [2, 'content-2']])
    render(
      <TimelineProvider value={{ ...defaultValues, contentMap }}>
        <TestConsumer>
          {(ctx) => <div data-testid="result">{ctx.contentMap.get(1)}|{ctx.contentMap.get(2)}</div>}
        </TestConsumer>
      </TimelineProvider>
    )
    expect(screen.getByTestId('result').textContent).toBe('content-1|content-2')
  })

  it('throws when useTimelineContext is used outside provider', () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => {
      render(
        <TestConsumer>
          {(ctx) => <div>{ctx.filePath}</div>}
        </TestConsumer>
      )
    }).toThrow('useTimelineContext must be used within TimelineProvider')
    consoleSpy.mockRestore()
  })
})
