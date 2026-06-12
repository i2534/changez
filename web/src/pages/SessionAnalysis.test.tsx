import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import SessionAnalysis from './SessionAnalysis'
import * as client from '../api/client'

const mockSession = {
  sessionId: 'ses-abc123',
  status: 'completed',
  model: 'Qwen3.6',
  summary: 'This session implemented the new auth system.',
  changes: [
    {
      filePath: 'internal/auth/login.go',
      projectName: 'changez',
      action: 'create' as const,
      message: 'Add login handler',
      timestamp: '2026-06-10T10:00:00Z',
    },
    {
      filePath: 'internal/auth/handler.go',
      projectName: 'changez',
      action: 'update' as const,
      message: 'Update auth middleware',
      timestamp: '2026-06-10T10:05:00Z',
    },
    {
      filePath: 'internal/auth/types.go',
      projectName: 'changez',
      action: 'update' as const,
      message: 'Add types',
      timestamp: '2026-06-10T10:10:00Z',
    },
  ],
}

beforeEach(() => {
  vi.spyOn(client, 'getAISession').mockResolvedValue(mockSession)
})

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/projects/changez/sessions/ses-abc123']}>
      <Routes>
        <Route path="/projects/:project/sessions/:sessionId" element={<SessionAnalysis />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('SessionAnalysis loading', () => {
  it('shows loading skeleton initially', () => {
    vi.spyOn(client, 'getAISession').mockImplementation(() => new Promise(() => {}))
    const { container } = renderPage()
    const skeletons = container.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })
})

describe('SessionAnalysis error', () => {
  it('shows error state with retry button', async () => {
    vi.spyOn(client, 'getAISession').mockRejectedValue(new Error('Session not found'))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/Failed to load session data/i)).toBeInTheDocument()
    })
    expect(screen.getByText(/Retry/i)).toBeInTheDocument()
  })
})

describe('SessionAnalysis data', () => {
  it('displays session title and ID', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/ses-abc123/)).toBeInTheDocument()
    })
  })

  it('displays stat cards: changes count', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getAllByText('3')).toHaveLength(2)
    })
  })

  it('shows model name in stat card', async () => {
    renderPage()
    await waitFor(() => {
      const stats = screen.getAllByText('Qwen3.6')
      expect(stats.length).toBeGreaterThanOrEqual(1)
    })
  })

  it('shows AI session summary', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/This session implemented the new auth system/)).toBeInTheDocument()
    })
  })

  it('shows file change list', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('internal/auth/login.go')).toBeInTheDocument()
    })
    expect(screen.getByText('internal/auth/handler.go')).toBeInTheDocument()
    expect(screen.getByText('internal/auth/types.go')).toBeInTheDocument()
  })

  it('shows action labels (create/update)', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('[create]')).toBeInTheDocument()
    })
    const updates = screen.getAllByText('[update]')
    expect(updates).toHaveLength(2)
  })
})

describe('SessionAnalysis copy summary', () => {
  it('copies summary to clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/This session implemented the new auth system/)).toBeInTheDocument()
    })
    await userEvent.click(screen.getByText(/Copy/i))
    expect(writeText).toHaveBeenCalledWith(mockSession.summary)
  })
})

describe('SessionAnalysis generating state', () => {
  it('shows generating indicator when no summary', async () => {
    vi.spyOn(client, 'getAISession').mockResolvedValue({
      ...mockSession,
      summary: undefined as unknown as string,
    })
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/Generating/i)).toBeInTheDocument()
    })
  })
})
