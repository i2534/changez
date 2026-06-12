import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import TrendsAnalysis from './TrendsAnalysis'
import * as client from '../api/client'

beforeEach(() => {
  vi.spyOn(client, 'getAITrends').mockResolvedValue({
    period: '2026-06-05 ~ 2026-06-12',
    totalFiles: 46,
    totalChanges: 366,
    sourceBreakdown: { opencode: 366 },
    topFiles: [
      { filePath: 'internal/ai/worker.go', count: 78 },
    ],
    status: 'completed',
    summary: 'Test trend analysis summary.',
    model: 'Qwen3.6',
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

function renderTrends() {
  return render(
    <MemoryRouter initialEntries={['/projects/changez/trends']}>
      <Routes>
        <Route path="/projects/:project/trends" element={<TrendsAnalysis />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('TrendsAnalysis loading', () => {
  it('shows loading skeleton initially', () => {
    vi.spyOn(client, 'getAITrends').mockImplementation(() => new Promise(() => {}))
    const { container } = renderTrends()
    const skeletons = container.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })
})

describe('TrendsAnalysis error', () => {
  it('shows error state with retry button', async () => {
    vi.spyOn(client, 'getAITrends').mockRejectedValue(new Error('Network failure'))
    renderTrends()
    await waitFor(() => expect(screen.getByText(/Network failure/i)).toBeInTheDocument())
    expect(screen.getByText(/Retry/i)).toBeInTheDocument()
  })
})

describe('TrendsAnalysis data', () => {
  it('displays stat cards', async () => {
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText('366')).toBeInTheDocument()
    })
    expect(screen.getByText('46')).toBeInTheDocument()
  })

  it('displays AI summary when completed', async () => {
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText(/Test trend analysis summary/i)).toBeInTheDocument()
    })
  })

  it('shows model name', async () => {
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText(/Qwen3.6/i)).toBeInTheDocument()
    })
  })

  it('shows source distribution', async () => {
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText('opencode')).toBeInTheDocument()
    })
  })

  it('shows top files table', async () => {
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText('internal/ai/worker.go')).toBeInTheDocument()
    })
  })
})

describe('TrendsAnalysis pending state', () => {
  it('shows generating indicator when pending', async () => {
    vi.spyOn(client, 'getAITrends').mockResolvedValue({
      period: '',
      totalFiles: 0,
      totalChanges: 0,
      sourceBreakdown: {},
      topFiles: [],
      status: 'pending',
    })
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText(/Generating AI analysis/i)).toBeInTheDocument()
    })
  })
})

describe('TrendsAnalysis query button', () => {
  it('re-fetches data on query click', async () => {
    const spy = vi.spyOn(client, 'getAITrends')
    renderTrends()
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1))
    const queryBtn = screen.getByText(/Query/i)
    await userEvent.click(queryBtn)
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2))
  })
})

describe('TrendsAnalysis summary copy', () => {
  it('copies summary to clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    renderTrends()
    await waitFor(() => {
      expect(screen.getByText(/Test trend analysis summary/i)).toBeInTheDocument()
    })
    await userEvent.click(screen.getByText(/Copy/i))
    expect(writeText).toHaveBeenCalledWith('Test trend analysis summary.')
  })
})
