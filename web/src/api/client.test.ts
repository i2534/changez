import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import {
  getToken,
  setToken,
  clearToken,
  checkAuthRequired,
  api,
  apiJSON,
  encodeFilePath,
  getAISummaries,
  refreshAISummary,
  getAISession,
  getAITrends,
} from './client'

describe('token storage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('returns null when no token stored', () => {
    expect(getToken()).toBeNull()
  })

  it('stores and retrieves token', () => {
    setToken('abc123')
    expect(getToken()).toBe('abc123')
  })

  it('clears token', () => {
    setToken('abc123')
    clearToken()
    expect(getToken()).toBeNull()
  })
})

describe('checkAuthRequired', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('returns true when API says required', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ required: true }), { status: 200 })
    ))
    await expect(checkAuthRequired()).resolves.toBe(true)
  })

  it('returns false when API says not required', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ required: false }), { status: 200 })
    ))
    await expect(checkAuthRequired()).resolves.toBe(false)
  })

  it('returns false on non-200 response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('', { status: 500 })
    ))
    await expect(checkAuthRequired()).resolves.toBe(false)
  })

  it('returns false on network error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network')))
    await expect(checkAuthRequired()).resolves.toBe(false)
  })
})

describe('api()', () => {
  beforeEach(() => {
    localStorage.clear()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('attaches Authorization header when token present', async () => {
    setToken('my-token')
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await api('/api/files')

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/files',
      expect.objectContaining({
        headers: expect.any(Object),
      })
    )
    const callArgs = fetchMock.mock.calls[0][1] as RequestInit
    const headers = callArgs.headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer my-token')
  })

  it('omits Authorization header when no token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await api('/api/files')

    const callArgs = fetchMock.mock.calls[0][1] as RequestInit
    const headers = callArgs.headers as Record<string, string>
    expect(headers['Authorization']).toBeUndefined()
  })

  it('dispatches auth-required event and clears token on 401', async () => {
    setToken('old-token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('unauthorized', { status: 401 })
    ))

    const handler = vi.fn()
    window.addEventListener('auth-required', handler)

    await api('/api/files')

    expect(getToken()).toBeNull()
    expect(handler).toHaveBeenCalledOnce()
    window.removeEventListener('auth-required', handler)
  })

  it('handles Headers instance without throwing', async () => {
    setToken('my-token')
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const customHeaders = new Headers({ 'X-Custom': 'value' })
    await expect(api('/api/files', { headers: customHeaders })).resolves.toBeDefined()
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('merges plain object headers with base headers', async () => {
    setToken('my-token')
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await api('/api/files', { headers: { 'X-Custom': 'foo' } })

    const callArgs = fetchMock.mock.calls[0][1] as RequestInit
    const headers = callArgs.headers as Record<string, string>
    expect(headers['X-Custom']).toBe('foo')
    expect(headers['Content-Type']).toBe('application/json')
  })
})

describe('apiJSON()', () => {
  beforeEach(() => {
    localStorage.clear()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('parses JSON response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ foo: 'bar' }), { status: 200 })
    ))

    const result = await apiJSON<{ foo: string }>('/api/test')
    expect(result).toEqual({ foo: 'bar' })
  })

  it('throws on non-OK status with error text', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('server error', { status: 500 })
    ))

    await expect(apiJSON('/api/test')).rejects.toThrow(/500/)
  })

  it('throws on empty response body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('', { status: 200 })
    ))

    await expect(apiJSON('/api/test')).rejects.toThrow(/Empty response/)
  })

  it('throws when response is not valid JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('not json{', { status: 200 })
    ))

    await expect(apiJSON('/api/test')).rejects.toThrow()
  })
})

describe('encodeFilePath', () => {
  it('double-encodes path', () => {
    expect(encodeFilePath('/foo/bar')).toBe('%252Ffoo%252Fbar')
  })

  it('encodes special characters', () => {
    expect(encodeFilePath('a b/c')).toBe('a%2520b%252Fc')
  })
})

describe('getAISummaries', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('builds query string from all params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ summaries: [] }), { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await getAISummaries({
      project: 'changez',
      path: '/src',
      since: '2026-01-01',
      until: '2026-12-31',
      limit: 10,
      offset: 5,
    })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('project=changez')
    expect(url).toContain('path=')
    expect(url).toContain('since=2026-01-01')
    expect(url).toContain('until=2026-12-31')
    expect(url).toContain('limit=10')
    expect(url).toContain('offset=5')
  })

  it('omits missing params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ summaries: [] }), { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await getAISummaries({ project: 'changez' })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('project=changez')
    expect(url).not.toContain('path=')
    expect(url).not.toContain('limit=')
  })
})

describe('refreshAISummary', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('POSTs to refresh endpoint with query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await refreshAISummary({ project: 'changez', path: '/src', version: 42 })

    const url = fetchMock.mock.calls[0][0] as string
    const options = fetchMock.mock.calls[0][1] as RequestInit
    expect(url).toContain('/api/files/summary/refresh?')
    expect(url).toContain('project=changez')
    expect(url).toContain('path=')
    expect(url).toContain('version=42')
    expect(options.method).toBe('POST')
  })
})

describe('getAISession', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('includes sessionId and project', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await getAISession({ project: 'changez', sessionId: 'ses-abc' })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('sessionId=ses-abc')
    expect(url).toContain('project=changez')
  })
})

describe('getAITrends', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('builds trends query', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await getAITrends({
      project: 'changez',
      since: '2026-06-01',
      until: '2026-06-12',
      topFiles: 10,
    })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('/api/files/trends?')
    expect(url).toContain('project=changez')
    expect(url).toContain('since=2026-06-01')
    expect(url).toContain('until=2026-06-12')
    expect(url).toContain('topFiles=10')
  })

  it('omits topFiles when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{}', { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    await getAITrends({ project: 'changez' })

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).not.toContain('topFiles=')
  })
})
