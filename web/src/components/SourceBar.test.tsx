import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import SourceBar from './SourceBar'

describe('SourceBar', () => {
  it('returns null when sources are empty', () => {
    const { container } = render(<SourceBar sources={{}} />)
    expect(container.innerHTML).toBe('')
  })

  it('shows heading', () => {
    render(<SourceBar sources={{ opencode: 10 }} />)
    expect(screen.getByText('Change Sources')).toBeTruthy()
  })

  it('renders source with count and percentage', () => {
    render(<SourceBar sources={{ opencode: 75, cursor: 25 }} />)
    expect(screen.getByText('opencode')).toBeTruthy()
    expect(screen.getByText('cursor')).toBeTruthy()
    expect(screen.getByText('75 (75%)')).toBeTruthy()
    expect(screen.getByText('25 (25%)')).toBeTruthy()
  })

  it('sorts sources by count descending', () => {
    render(<SourceBar sources={{ a: 10, b: 50, c: 30 }} />)
    const rows = screen.getAllByText(/\(\d+%\)/)
    expect(rows[0].textContent).toBe('50 (56%)')
    expect(rows[1].textContent).toBe('30 (33%)')
    expect(rows[2].textContent).toBe('10 (11%)')
  })

  it('handles single source as 100%', () => {
    render(<SourceBar sources={{ opencode: 42 }} />)
    expect(screen.getByText('42 (100%)')).toBeTruthy()
  })

  it('handles large numbers with locale formatting', () => {
    render(<SourceBar sources={{ opencode: 1000, cursor: 500 }} />)
    expect(screen.getByText('1,000 (67%)')).toBeTruthy()
    expect(screen.getByText('500 (33%)')).toBeTruthy()
  })
})
