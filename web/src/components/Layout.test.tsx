import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import Layout from './Layout'
import * as client from '../api/client'

describe('Layout auth', () => {
  it('opens LoginModal when auth required and no token', async () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(true)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    await waitFor(() => expect(screen.getByText(/Authentication Required/i)).toBeInTheDocument())
  })

  it('does not open LoginModal when auth not required', async () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    await waitFor(() => expect(screen.queryByText(/Authentication Required/i)).not.toBeInTheDocument())
  })

  it('does not open LoginModal when token stored', async () => {
    localStorage.setItem('changez_token', 'existing-token')
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(true)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByText(/Authentication Required/i)).not.toBeInTheDocument()
  })

  it('opens LoginModal on auth-required event', async () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    await waitFor(() => expect(screen.queryByText(/Authentication Required/i)).not.toBeInTheDocument())
    act(() => window.dispatchEvent(new CustomEvent('auth-required')))
    expect(screen.getByText(/Authentication Required/i)).toBeInTheDocument()
  })
})

describe('Layout navigation', () => {
  it('renders app name and language toggle', () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    expect(screen.getByText('Changez')).toBeInTheDocument()
    expect(screen.getByText('中文')).toBeInTheDocument()
  })

  it('hides trends link outside project routes', () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    expect(screen.queryByText(/Trends Analysis/i)).not.toBeInTheDocument()
  })

  it('shows trends link inside project routes', () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/projects/changez']}><Routes><Route path="/projects/:project" element={<Layout><div>project</div></Layout>} /></Routes></MemoryRouter>)
    expect(screen.getByText(/Trends Analysis/i)).toBeInTheDocument()
  })

  it('toggles language', async () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes><Route path="/" element={<Layout><div>home</div></Layout>} /></Routes></MemoryRouter>)
    const langBtn = screen.getByText('中文')
    await userEvent.click(langBtn)
    expect(screen.getByText('EN')).toBeInTheDocument()
  })
})
