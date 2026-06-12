import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import LoginModal from './LoginModal'
import * as client from '../api/client'

describe('LoginModal', () => {
  const onClose = vi.fn()

  beforeEach(() => {
    onClose.mockClear()
    localStorage.clear()
    // mock window.location.reload
    Object.defineProperty(window, 'location', {
      value: { reload: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders nothing when closed', () => {
    const { container } = render(<LoginModal open={false} onClose={onClose} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders form when open', () => {
    render(<LoginModal open={true} onClose={onClose} />)
    expect(screen.getByPlaceholderText(/token/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /connect/i })).toBeInTheDocument()
  })

  it('does not submit on empty token', async () => {
    const apiSpy = vi.spyOn(client, 'api')
    render(<LoginModal open={true} onClose={onClose} />)

    const submitBtn = screen.getByRole('button', { name: /connect/i })
    expect(submitBtn).toBeDisabled()
    expect(apiSpy).not.toHaveBeenCalled()
  })

  it('saves token and reloads page on successful verification', async () => {
    const reloadSpy = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { reload: reloadSpy },
      writable: true,
    })

    vi.spyOn(client, 'api').mockResolvedValue(new Response('ok', { status: 200 }))

    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText(/token/i), 'my-secret')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    await waitFor(() => {
      expect(client.getToken()).toBe('my-secret')
    })
    expect(reloadSpy).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalled()
  })

  it('shows error message on invalid token (401)', async () => {
    vi.spyOn(client, 'api').mockResolvedValue(
      new Response('unauthorized', { status: 401 })
    )

    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText(/token/i), 'wrong')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    await waitFor(() => {
      expect(screen.getByText(/invalid|无效/i)).toBeInTheDocument()
    })
    expect(onClose).not.toHaveBeenCalled()
    expect((window.location as { reload: () => void }).reload).not.toHaveBeenCalled()
  })

  it('shows error on network failure', async () => {
    vi.spyOn(client, 'api').mockRejectedValue(new Error('network down'))

    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText(/token/i), 'something')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    await waitFor(() => {
      expect(screen.getByText(/cannot connect|无法连接/i)).toBeInTheDocument()
    })
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes on Escape key', async () => {
    render(<LoginModal open={true} onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('cancel button calls onClose', async () => {
    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)
    await user.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows verifying state during submission', async () => {
    let resolveApi: (value: Response) => void
    vi.spyOn(client, 'api').mockImplementation(
      () => new Promise<Response>((resolve) => { resolveApi = resolve })
    )

    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText(/token/i), 'token')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    expect(screen.getByRole('button', { name: /verifying/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /verifying/i })).toBeDisabled()

    resolveApi!(new Response('ok', { status: 200 }))
  })

  it('trims whitespace from token before saving', async () => {
    const reloadSpy = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { reload: reloadSpy },
      writable: true,
    })

    vi.spyOn(client, 'api').mockResolvedValue(new Response('ok', { status: 200 }))

    const user = userEvent.setup()
    render(<LoginModal open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText(/token/i), '  my-token  ')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    await waitFor(() => {
      expect(client.getToken()).toBe('my-token')
    })
  })
})
