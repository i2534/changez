import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Layout from './Layout'
import * as client from '../api/client'

describe('Layout breadcrumbs', () => {
  it('shows dashboard as root breadcrumb', () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/']}><Routes>
      <Route path="/" element={<Layout><div>home child</div></Layout>} />
      <Route path="/projects/:project" element={<Layout><div>project child</div></Layout>} />
      <Route path="/projects/:project/files" element={<Layout><div>files child</div></Layout>} />
      <Route path="/projects/:project/files/*" element={<Layout><div>file child</div></Layout>} />
      <Route path="/projects/:project/diff" element={<Layout><div>diff child</div></Layout>} />
    </Routes></MemoryRouter>)
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('shows project name in breadcrumb for project files path', () => {
    vi.spyOn(client, 'checkAuthRequired').mockResolvedValue(false)
    render(<MemoryRouter initialEntries={['/projects/changez/files']}><Routes>
      <Route path="/" element={<Layout><div>home child</div></Layout>} />
      <Route path="/projects/:project" element={<Layout><div>project child</div></Layout>} />
      <Route path="/projects/:project/files" element={<Layout><div>files child</div></Layout>} />
      <Route path="/projects/:project/files/*" element={<Layout><div>file child</div></Layout>} />
      <Route path="/projects/:project/diff" element={<Layout><div>diff child</div></Layout>} />
    </Routes></MemoryRouter>)
    expect(screen.getAllByText('changez').length).toBeGreaterThan(0)
  })
})
