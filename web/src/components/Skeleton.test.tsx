import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import Skeleton from './Skeleton'

describe('Skeleton', () => {
  it('renders with default row variant', () => {
    const { container } = render(<Skeleton />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('animate-pulse')
    expect(el.className).toContain('h-10')
    expect(el.className).toContain('rounded')
  })

  it('renders card variant', () => {
    const { container } = render(<Skeleton variant="card" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('h-24')
    expect(el.className).toContain('rounded-lg')
  })

  it('renders block variant', () => {
    const { container } = render(<Skeleton variant="block" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('h-64')
  })

  it('renders timeline variant', () => {
    const { container } = render(<Skeleton variant="timeline" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('h-16')
  })

  it('applies additional className', () => {
    const { container } = render(<Skeleton className="extra-class" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('extra-class')
  })
})
