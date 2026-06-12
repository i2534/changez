import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TimelineFilter } from './TimelineFilter'

function makeProps(overrides = {}) {
  return {
    sources: ['opencode', 'cursor', 'human'],
    sourceFilter: '',
    actionFilter: '',
    hasFilters: false,
    filteredCount: 10,
    totalCount: 100,
    versionJumpRef: { current: null },
    versionJump: '',
    onVersionJumpChange: vi.fn(),
    onVersionJumpSubmit: vi.fn(),
    onSourceChange: vi.fn(),
    onActionChange: vi.fn(),
    onClear: vi.fn(),
    ...overrides,
  }
}

describe('TimelineFilter', () => {
  it('renders version jump input with placeholder', () => {
    render(<TimelineFilter {...makeProps()} />)
    expect(screen.getByPlaceholderText('Jump to version...')).toBeTruthy()
  })

  it('renders source and action selects with options', () => {
    render(<TimelineFilter {...makeProps()} />)
    const selects = screen.getAllByRole('combobox')
    expect(selects.length).toBe(2)
    // Source select has the source options
    expect(screen.getByText('All Sources')).toBeTruthy()
    expect(screen.getByText('opencode')).toBeTruthy()
    expect(screen.getByText('cursor')).toBeTruthy()
    expect(screen.getByText('human')).toBeTruthy()
    // Action select has the action options
    expect(screen.getByText('All Actions')).toBeTruthy()
    expect(screen.getByText('create')).toBeTruthy()
    expect(screen.getByText('update')).toBeTruthy()
    expect(screen.getByText('delete')).toBeTruthy()
  })

  it('does not show clear button when no filters active', () => {
    render(<TimelineFilter {...makeProps()} />)
    expect(screen.queryByText('Clear')).toBeNull()
  })

  it('shows clear button when filters are active', () => {
    render(<TimelineFilter {...makeProps({ hasFilters: true, sourceFilter: 'opencode' })} />)
    expect(screen.getByText('Clear')).toBeTruthy()
  })

  it('shows filter count when filters are active', () => {
    render(<TimelineFilter {...makeProps({ hasFilters: true, sourceFilter: 'opencode' })} />)
    expect(screen.getByText('10 / 100')).toBeTruthy()
  })

  it('calls onClear when clear button clicked', () => {
    const onClear = vi.fn()
    render(<TimelineFilter {...makeProps({ hasFilters: true, sourceFilter: 'opencode', onClear })} />)
    fireEvent.click(screen.getByText('Clear'))
    expect(onClear).toHaveBeenCalledTimes(1)
  })

  it('calls onSourceChange on source select change', () => {
    const onSourceChange = vi.fn()
    render(<TimelineFilter {...makeProps({ onSourceChange })} />)
    const selects = screen.getAllByRole('combobox')
    fireEvent.change(selects[0], { target: { value: 'cursor' } })
    expect(onSourceChange).toHaveBeenCalledWith('cursor')
  })

  it('calls onActionChange on action select change', () => {
    const onActionChange = vi.fn()
    render(<TimelineFilter {...makeProps({ onActionChange })} />)
    const selects = screen.getAllByRole('combobox')
    fireEvent.change(selects[1], { target: { value: 'delete' } })
    expect(onActionChange).toHaveBeenCalledWith('delete')
  })

  it('calls onVersionJumpSubmit on Enter key', () => {
    const onVersionJumpSubmit = vi.fn()
    render(<TimelineFilter {...makeProps({ onVersionJumpSubmit })} />)
    const input = screen.getByPlaceholderText('Jump to version...')
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onVersionJumpSubmit).toHaveBeenCalledTimes(1)
  })

  it('does not submit on other keys', () => {
    const onVersionJumpSubmit = vi.fn()
    render(<TimelineFilter {...makeProps({ onVersionJumpSubmit })} />)
    const input = screen.getByPlaceholderText('Jump to version...')
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(onVersionJumpSubmit).not.toHaveBeenCalled()
  })
})
