import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ActivityFeed from './ActivityFeed'
import { ActivityItem } from '../api/types'

function makeItem(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    fileId: 1,
    filePath: 'src/main.ts',
    projectName: 'test',
    projectId: 1,
    versionId: 1,
    action: 'update',
    source: 'opencode',
    timestamp: new Date().toISOString(),
    ...overrides,
  }
}

describe('ActivityFeed empty state', () => {
  it('shows heading and empty message when no items', () => {
    render(<ActivityFeed items={[]} onFileClick={() => {}} />)
    expect(screen.getByText('Recent Activity')).toBeTruthy()
    expect(screen.getByText('No recent activity.')).toBeTruthy()
  })
})

describe('ActivityFeed rendering', () => {
  it('renders a single activity item', () => {
    const items = [makeItem({ filePath: 'src/main.ts', source: 'opencode' })]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)
    expect(screen.getByText('src/main.ts')).toBeTruthy()
    expect(screen.getByText('opencode')).toBeTruthy()
    expect(screen.getByText('update')).toBeTruthy()
  })

  it('shows relative time for activity', () => {
    const items = [makeItem()]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)
    expect(screen.getByText(/ago/)).toBeTruthy()
  })

  it('merges consecutive updates to same file into one group', () => {
    const items = [
      makeItem({ filePath: 'src/main.ts', timestamp: '2026-06-12T10:00:00Z' }),
      makeItem({ filePath: 'src/main.ts', timestamp: '2026-06-12T10:01:00Z' }),
    ]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    // Should show single row with count badge
    expect(screen.getByText('×2')).toBeTruthy()
    // Only one file path rendered
    const paths = screen.getAllByText('src/main.ts')
    expect(paths.length).toBe(1)
  })

  it('does not merge non-consecutive items for same file', () => {
    const items = [
      makeItem({ filePath: 'src/main.ts', source: 'opencode', timestamp: '2026-06-12T10:00:00Z' }),
      makeItem({ filePath: 'src/utils.ts', source: 'opencode', timestamp: '2026-06-12T10:01:00Z' }),
      makeItem({ filePath: 'src/main.ts', source: 'opencode', timestamp: '2026-06-12T10:02:00Z' }),
    ]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    // Two separate groups for main.ts
    const mainPaths = screen.getAllByText('src/main.ts')
    expect(mainPaths.length).toBe(2)
  })

  it('shows action summary for merged groups with multiple action types', () => {
    const items = [
      makeItem({ filePath: 'src/main.ts', action: 'create' }),
      makeItem({ filePath: 'src/main.ts', action: 'update' }),
      makeItem({ filePath: 'src/main.ts', action: 'delete' }),
    ]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    expect(screen.getByText('3 changes')).toBeTruthy()
  })

  it('shows action count for merged groups with same action', () => {
    const items = [
      makeItem({ filePath: 'src/main.ts', action: 'update' }),
      makeItem({ filePath: 'src/main.ts', action: 'update' }),
      makeItem({ filePath: 'src/main.ts', action: 'update' }),
    ]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    expect(screen.getByText('3 updates')).toBeTruthy()
  })

  it('does not show count badge for single item groups', () => {
    const items = [makeItem({ filePath: 'src/main.ts' })]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    expect(screen.queryByText(/×/)).toBeNull()
  })

  it('separates groups by different projects', () => {
    const items = [
      makeItem({ filePath: 'src/main.ts', projectName: 'project-a' }),
      makeItem({ filePath: 'src/main.ts', projectName: 'project-b' }),
    ]
    render(<ActivityFeed items={items} onFileClick={() => {}} />)

    const paths = screen.getAllByText('src/main.ts')
    expect(paths.length).toBe(2)
  })
})

describe('ActivityFeed callbacks', () => {
  it('calls onFileClick with project and path when item clicked', () => {
    const onFileClick = vi.fn()
    const items = [makeItem({ projectName: 'proj', filePath: 'src/main.ts' })]
    render(<ActivityFeed items={items} onFileClick={onFileClick} />)

    fireEvent.click(screen.getByText('src/main.ts'))
    expect(onFileClick).toHaveBeenCalledTimes(1)
    expect(onFileClick).toHaveBeenCalledWith('proj', 'src/main.ts')
  })
})
