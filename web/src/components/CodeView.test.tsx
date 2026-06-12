import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import CodeView from './CodeView'

describe('CodeView', () => {
  it('renders file path', () => {
    render(<CodeView content="hello" filePath="src/main.ts" />)
    expect(screen.getByText('src/main.ts')).toBeTruthy()
  })

  it('shows language badge for known file extension', () => {
    render(<CodeView content="hello" filePath="src/main.ts" />)
    expect(screen.getByText('typescript')).toBeTruthy()
  })

  it('shows language badge for go files', () => {
    render(<CodeView content="package main" filePath="main.go" />)
    expect(screen.getByText('go')).toBeTruthy()
  })

  it('hides language badge for unknown extensions', () => {
    render(<CodeView content="data" filePath="data.xyz" />)
    expect(screen.queryByText('typescript')).toBeNull()
    expect(screen.queryByText('go')).toBeNull()
  })

  it('shows Empty file for empty content', () => {
    render(<CodeView content="" filePath="test.ts" />)
    expect(screen.getByText('Empty file')).toBeTruthy()
  })

  it('renders line number for first line', () => {
    render(<CodeView content="line1\nline2\nline3" filePath="test.ts" />)
    expect(screen.getByText('1')).toBeTruthy()
  })

  it('renders code content with syntax highlighting', () => {
    render(<CodeView content="const x: number = 1;" filePath="test.ts" />)
    // Prism should highlight the keyword 'const'
    const highlighted = document.querySelector('.token.keyword')
    expect(highlighted).toBeTruthy()
    expect(highlighted?.textContent).toBe('const')
  })

  it('highlights Go language keywords', () => {
    render(<CodeView content={'package main\n\nfunc main() {}'} filePath="main.go" />)
    const keywords = document.querySelectorAll('.token.keyword')
    expect(keywords.length).toBeGreaterThan(0)
  })

  it('escapes HTML in unknown language content', () => {
    render(<CodeView content={'<script>alert("xss")</script>'} filePath="file.txt" />)
    // The HTML entities are decoded in the DOM text content; tags rendered as text
    expect(screen.getByText(/script>alert/)).toBeTruthy()
    // No actual script tag executed
    const scriptTags = document.querySelectorAll('script')
    const codeScripts = Array.from(scriptTags).filter((s) => !s.src)
    expect(codeScripts.length).toBe(0)
  })

  it('applies custom height', () => {
    const { container } = render(<CodeView content="line1\nline2" filePath="test.ts" height={100} />)
    // The virtual list container should have the custom height
    const outerEl = container.querySelector('[style*="height"]')
    expect(outerEl).toBeTruthy()
  })
})
