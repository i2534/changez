import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TimelineRow } from './TimelineRow'
import { TimelineProvider, TimelineContextValue } from './TimelineContext'

// react-window 2.x rowComponent receives { ariaAttributes, index, style } & RowProps
// RowProps are SPREAD directly into props (not nested under "data")
const style = { top: 0, left: 0, width: 800, height: 60 }

function makeEntry(overrides = {}) {
  return {
    versionId: 5,
    filePath: '/src/main.ts',
    project: 'test',
    timestamp: new Date('2024-06-01T10:00:00Z').toISOString(),
    source: 'opencode',
    action: 'update' as const,
    sessionId: undefined,
    model: undefined,
    message: undefined,
    ...overrides,
  }
}

const defaultRowProps = {
  filtered: [makeEntry()],
  onVersionClick: vi.fn(),
  onRowClick: vi.fn(),
  onDiff: vi.fn(),
  fetchInlineContent: vi.fn(async () => {}),
  onCopyContent: vi.fn(),
}

function mockT(key: string, opts?: Record<string, unknown>) {
  const map: Record<string, string> = {
    'timeline.view_content': `View content at v${opts?.version ?? ''}`,
    'timeline.hide_content': `Hide content at v${opts?.version ?? ''}`,
    'timeline.show_details': 'Show details',
    'timeline.hide_details': 'Hide details',
    'timeline.diff_between': `Diff ${opts?.from} ↔ ${opts?.to}`,
    'timeline.copy_clipboard': `Copy v${opts?.version ?? ''}`,
    'timeline.ai_summary': 'AI Summary',
    'timeline.content_at_v': `Content at v${opts?.version ?? ''}`,
    'timeline.loading_content': 'Loading...',
    'timeline.ai_summary_generating': 'Generating...',
    'timeline.session': 'Session',
    'timeline.model': 'Model',
    'timeline.message': 'Message',
  }
  return map[key] ?? key
}

function defaultContext(overrides = {}): TimelineContextValue {
  return {
    expandedId: null,
    setExpandedId: vi.fn(),
    contentVersionId: null,
    setContentVersionId: vi.fn(),
    contentMap: new Map(),
    loadingContent: null,
    selectedIds: [],
    filePath: '/src/main.ts',
    project: 'test',
    t: mockT,
    summaries: new Map(),
    ...overrides,
  }
}

function renderRow(rowProps = defaultRowProps, contextOverrides = {}) {
  return render(
    <TimelineProvider value={defaultContext(contextOverrides)}>
      <TimelineRow index={0} style={style} ariaAttributes={{ "aria-posinset": 1, "aria-setsize": 1, role: "listitem" }} {...rowProps} />
    </TimelineProvider>
  )
}

describe('TimelineRow', () => {
  it('renders version number', () => {
    renderRow()
    expect(screen.getByText('v5')).toBeTruthy()
  })

  it('renders source badge', () => {
    renderRow()
    expect(screen.getByText('opencode')).toBeTruthy()
  })

  it('renders action text', () => {
    renderRow()
    expect(screen.getByText('update')).toBeTruthy()
  })

  it('calls onRowClick when row clicked', () => {
    const onRowClick = vi.fn()
    renderRow({ ...defaultRowProps, onRowClick })
    fireEvent.click(screen.getByText('v5').closest('[class*="rounded-lg"]')!)
    expect(onRowClick).toHaveBeenCalledWith(0)
  })

  it('calls onVersionClick when version number clicked', () => {
    const onVersionClick = vi.fn()
    renderRow({ ...defaultRowProps, onVersionClick })
    fireEvent.click(screen.getByText('v5'))
    expect(onVersionClick).toHaveBeenCalledWith(5, false)
  })

  it('shows selected state styling', () => {
    renderRow(defaultRowProps, { selectedIds: [5] })
    const card = screen.getByText('v5').closest('[class*="rounded-lg"]')!
    expect(card.className).toContain('border-l-2')
    expect(card.className).toContain('border-blue-500')
  })

  it('shows Show details button when entry has meta', () => {
    renderRow({
      ...defaultRowProps,
      filtered: [makeEntry({ sessionId: 'ses_1' })],
    })
    expect(screen.getByText('Show details')).toBeTruthy()
  })

  it('shows Hide details when expanded', () => {
    renderRow({
      ...defaultRowProps,
      filtered: [makeEntry({ sessionId: 'ses_1' })],
    }, { expandedId: 5 })
    expect(screen.getByText('Hide details')).toBeTruthy()
  })

  it('shows AI summary when present', () => {
    const summaries = new Map([[5, { summary: 'Added a new feature', status: 'completed' as const }]])
    renderRow(defaultRowProps, { expandedId: 5, summaries })
    expect(screen.getByText('Added a new feature')).toBeTruthy()
  })

  it('shows inline content toggle button', () => {
    renderRow()
    expect(screen.getByText('View content at v5')).toBeTruthy()
  })

  it('shows inline content when contentVersionId matches', () => {
    const contentMap = new Map([[5, 'file content here']])
    renderRow(defaultRowProps, { contentVersionId: 5, contentMap })
    expect(screen.getByText('file content here')).toBeTruthy()
  })

  it('shows diff button when 2 selected and current is in selection', () => {
    renderRow(defaultRowProps, { selectedIds: [3, 5] })
    expect(screen.getByText(/Diff/)).toBeTruthy()
  })

  it('calls onDiff when diff button clicked', () => {
    const onDiff = vi.fn()
    renderRow({ ...defaultRowProps, onDiff }, { selectedIds: [3, 5] })
    fireEvent.click(screen.getByText(/Diff/))
    expect(onDiff).toHaveBeenCalledTimes(1)
  })

  it('hides diff button when only 1 selected', () => {
    renderRow(defaultRowProps, { selectedIds: [5] })
    expect(screen.queryByText(/Diff/)).toBeNull()
  })

  it('calls setContentVersionId when content toggle clicked', () => {
    const setContentVersionId = vi.fn()
    renderRow(defaultRowProps, { setContentVersionId })
    fireEvent.click(screen.getByText('View content at v5'))
    expect(setContentVersionId).toHaveBeenCalledWith(5)
  })
})
