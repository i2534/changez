import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import DiffViewer from './DiffViewer'

const mockDraw = vi.fn()

class MockDiff2HtmlUI {
  constructor(_el: HTMLElement, _diff: string, _opts: Record<string, unknown>) {
    this.draw = mockDraw
  }
  draw: ReturnType<typeof vi.fn>
}

vi.mock('diff2html/bundles/js/diff2html-ui.min.js', () => ({
  Diff2HtmlUI: MockDiff2HtmlUI,
}))

describe('DiffViewer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders version header', () => {
    render(<DiffViewer diff="diff content" fromVersion={3} toVersion={5} />)
    expect(screen.getByText((_content, el) => {
      if (!el || el.tagName !== 'SPAN') return false
      return el.textContent?.includes('v3') && el.textContent?.includes('v5')
    })).toBeTruthy()
  })

  it('renders diff container', () => {
    const { container } = render(<DiffViewer diff="diff content" fromVersion={1} toVersion={2} />)
    expect(container.querySelector('.d2h-dark-color-scheme')).toBeTruthy()
  })

  it('calls Diff2HtmlUI draw', async () => {
    render(<DiffViewer diff="--- a/file\n+++ b/file" fromVersion={1} toVersion={2} />)
    await vi.waitFor(() => {
      expect(mockDraw).toHaveBeenCalled()
    })
  })
})
