import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import FileList from './FileList'
import { File } from '../api/types'

function makeFile(path: string, latestVersionId: number | null = 1): File {
  return {
    project: 'test',
    path,
    latestVersionId,
    createdAt: new Date().toISOString(),
  }
}

// --- buildTree (tested through component rendering) ---

describe('FileList tree building', () => {
  it('shows empty state for empty files', () => {
    render(<FileList files={[]} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByText('No files found.')).toBeTruthy()
  })

  it('renders a flat file as a leaf node', () => {
    const files = [makeFile('main.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByText('main.ts')).toBeTruthy()
  })

  it('renders files in a directory under a folder node', () => {
    const files = [makeFile('src/main.ts'), makeFile('src/utils.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    // Directory name should be visible
    expect(screen.getByText('src')).toBeTruthy()
    // Files inside shouldn't be visible until expanded
    expect(screen.queryByText('main.ts')).toBeNull()
    expect(screen.queryByText('utils.ts')).toBeNull()
  })

  it('expands directory and shows children when clicked', () => {
    const files = [makeFile('src/main.ts'), makeFile('src/utils.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    fireEvent.click(screen.getByText('src'))
    expect(screen.getByText('main.ts')).toBeTruthy()
    expect(screen.getByText('utils.ts')).toBeTruthy()
  })

  it('renders nested directories', () => {
    const files = [makeFile('src/components/Button.tsx'), makeFile('src/lib/helper.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    expect(screen.getByText('src')).toBeTruthy()
    expect(screen.queryByText('components')).toBeNull()
    expect(screen.queryByText('lib')).toBeNull()

    // Expand src
    fireEvent.click(screen.getByText('src'))
    expect(screen.getByText('components')).toBeTruthy()
    expect(screen.getByText('lib')).toBeTruthy()

    // Expand components
    fireEvent.click(screen.getByText('components'))
    expect(screen.getByText('Button.tsx')).toBeTruthy()
  })

  it('sorts directories before files', () => {
    const files = [
      makeFile('z-file.ts'),
      makeFile('src/main.ts'),
    ]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    // src (dir) should come before z-file.ts
    const items = screen.getAllByRole('button')
    const srcIdx = items.findIndex((b) => b.textContent?.includes('src'))
    const fileIdx = items.findIndex((b) => b.textContent?.includes('z-file.ts'))
    expect(srcIdx).toBeLessThan(fileIdx)
  })

  it('sorts files alphabetically within a directory', () => {
    const files = [makeFile('src/alpha.ts'), makeFile('src/beta.ts'), makeFile('src/gamma.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    fireEvent.click(screen.getByText('src'))
    const fileButtons = screen.getAllByRole('button')
      .filter((b) => b.textContent?.trim().startsWith('alpha') || b.textContent?.trim().startsWith('beta') || b.textContent?.trim().startsWith('gamma'))
    const texts = fileButtons.map((b) => b.textContent!.split('v')[0].trim())
    expect(texts).toEqual(['alpha.ts', 'beta.ts', 'gamma.ts'])
  })
})

// --- Search / Filter ---

describe('FileList search & filter', () => {
  const files = [
    makeFile('src/main.ts'),
    makeFile('src/utils.ts'),
    makeFile('README.md'),
  ]

  it('shows search input with placeholder', () => {
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByPlaceholderText('Search files...')).toBeTruthy()
  })

  it('calls onSearchChange when typing', () => {
    const onSearchChange = vi.fn()
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={onSearchChange} />)

    fireEvent.change(screen.getByPlaceholderText('Search files...'), { target: { value: 'main' } })
    expect(onSearchChange).toHaveBeenCalledWith('main')
  })

  it('shows only matching files when searchQuery is set', () => {
    const { rerender } = render(
      <FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />
    )
    expect(screen.getByText('src')).toBeTruthy()
    expect(screen.getByText('README.md')).toBeTruthy()

    rerender(<FileList files={files} onFileClick={() => {}} searchQuery="main" onSearchChange={() => {}} />)
    // src directory should still be visible (it contains matching file)
    expect(screen.getByText('src')).toBeTruthy()
    expect(screen.queryByText('README.md')).toBeNull()
  })

  it('shows empty state when no files match search', () => {
    render(<FileList files={files} onFileClick={() => {}} searchQuery="nonexistent" onSearchChange={() => {}} />)
    expect(screen.getByText('No files found.')).toBeTruthy()
  })

  it('removes empty state when search changes to empty', () => {
    const { rerender } = render(
      <FileList
        files={files}
        onFileClick={() => {}}
        searchQuery="nonexistent"
        onSearchChange={() => {}}
      />
    )
    expect(screen.getByText('No files found.')).toBeTruthy()

    rerender(
      <FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />
    )
    expect(screen.queryByText('No files found.')).toBeNull()
  })
})

// --- Callbacks ---

describe('FileList callbacks', () => {
  it('calls onFileClick when a file is clicked', () => {
    const onFileClick = vi.fn()
    const files = [makeFile('src/main.ts')]
    render(<FileList files={files} onFileClick={onFileClick} searchQuery="" onSearchChange={() => {}} />)

    fireEvent.click(screen.getByText('src'))
    fireEvent.click(screen.getByText('main.ts'))
    expect(onFileClick).toHaveBeenCalledTimes(1)
    expect(onFileClick).toHaveBeenCalledWith('src/main.ts')
  })

  it('shows delete button when onFileDelete is provided', () => {
    const files = [makeFile('src/main.ts')]
    render(
      <FileList files={files} onFileClick={() => {}} onFileDelete={() => {}} searchQuery="" onSearchChange={() => {}} />
    )

    fireEvent.click(screen.getByText('src'))
    // Delete icon should be present
    const deleteButtons = screen.getAllByRole('button').filter(
      (b) => b.querySelector('svg')
    )
    expect(deleteButtons.length).toBeGreaterThan(0)
  })

  it('does not show delete button when onFileDelete is not provided', () => {
    const files = [makeFile('src/main.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    fireEvent.click(screen.getByText('src'))
    const deleteButtons = screen.getAllByRole('button').filter(
      (b) => b.querySelector('svg')
    )
    // Only the chevron + folder/file icons should exist (no trash)
    const allSvgButtons = deleteButtons.filter((b) => {
      const html = b.innerHTML
      return html.includes('trash') || html.includes('Trash') || html.includes('delete')
    })
    expect(allSvgButtons.length).toBe(0)
  })

  it('calls onFileDelete when delete icon is clicked', () => {
    const onFileDelete = vi.fn()
    const files = [makeFile('src/main.ts')]
    render(
      <FileList files={files} onFileClick={() => {}} onFileDelete={onFileDelete} searchQuery="" onSearchChange={() => {}} />
    )

    fireEvent.click(screen.getByText('src'))
    const deleteBtn = screen.getByTitle('Delete')
    fireEvent.click(deleteBtn)
    expect(onFileDelete).toHaveBeenCalledTimes(1)
    expect(onFileDelete).toHaveBeenCalledWith(
      expect.objectContaining({ path: 'src/main.ts' })
    )
  })
})

// --- Collapse / Expand ---

describe('FileList expand/collapse', () => {
  it('toggles directory collapse on click', () => {
    const files = [makeFile('src/main.ts')]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    expect(screen.queryByText('main.ts')).toBeNull()

    // Expand
    fireEvent.click(screen.getByText('src'))
    expect(screen.getByText('main.ts')).toBeTruthy()

    // Collapse
    fireEvent.click(screen.getByText('src'))
    expect(screen.queryByText('main.ts')).toBeNull()
  })

  it('renders multiple directories each independently expandable', () => {
    const files = [
      makeFile('src/a.ts'),
      makeFile('lib/b.ts'),
    ]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)

    // Expand src
    fireEvent.click(screen.getByText('src'))
    expect(screen.getByText('a.ts')).toBeTruthy()
    expect(screen.queryByText('b.ts')).toBeNull()

    // Expand lib (src stays open)
    fireEvent.click(screen.getByText('lib'))
    expect(screen.getByText('a.ts')).toBeTruthy()
    expect(screen.getByText('b.ts')).toBeTruthy()

    // Collapse src
    fireEvent.click(screen.getByText('src'))
    expect(screen.queryByText('a.ts')).toBeNull()
    expect(screen.getByText('b.ts')).toBeTruthy()
  })

  it('auto-expands directories when search matches a file inside', () => {
    const files = [
      makeFile('src/components/Button.tsx'),
      makeFile('src/components/Input.tsx'),
      makeFile('lib/helper.ts'),
    ]
    const { rerender } = render(
      <FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />
    )

    // Search for "Button" should auto-expand src > components
    rerender(
      <FileList files={files} onFileClick={() => {}} searchQuery="Button" onSearchChange={() => {}} />
    )
    // src and components should both be auto-expanded
    expect(screen.getByText('Button.tsx')).toBeTruthy()
  })
})

// --- Display Info ---

describe('FileList display info', () => {
  it('shows version number for files', () => {
    const files = [makeFile('main.ts', 42)]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByText('v42')).toBeTruthy()
  })

  it('shows relative time for files', () => {
    const files = [makeFile('main.ts', 1)]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByText(/ago/)).toBeTruthy()
  })

  it('handles files with null latestVersionId', () => {
    const files = [makeFile('main.ts', null)]
    render(<FileList files={files} onFileClick={() => {}} searchQuery="" onSearchChange={() => {}} />)
    expect(screen.getByText('main.ts')).toBeTruthy()
    // No version badge (no crash)
  })
})
