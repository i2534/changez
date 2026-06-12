import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ConfirmDialog from './ConfirmDialog'

describe('ConfirmDialog', () => {
  it('renders nothing when open is false', () => {
    const { container } = render(
      <ConfirmDialog open={false} title="Title" message="Message" onConfirm={() => {}} onCancel={() => {}} />
    )
    expect(container.innerHTML).toBe('')
  })

  it('renders title and message when open', () => {
    render(
      <ConfirmDialog open={true} title="Delete file?" message="Are you sure?" onConfirm={() => {}} onCancel={() => {}} />
    )
    expect(screen.getByText('Delete file?')).toBeTruthy()
    expect(screen.getByText('Are you sure?')).toBeTruthy()
  })

  it('renders Cancel and Confirm buttons', () => {
    render(
      <ConfirmDialog open={true} title="Title" message="Message" onConfirm={() => {}} onCancel={() => {}} />
    )
    expect(screen.getByText('Cancel')).toBeTruthy()
    expect(screen.getByText('Confirm')).toBeTruthy()
  })

  it('calls onCancel when Cancel button clicked', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    fireEvent.click(screen.getByText('Cancel'))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('calls onConfirm when Confirm button clicked', () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={onConfirm} onCancel={() => {}} />
    )
    fireEvent.click(screen.getByText('Confirm'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('calls onCancel when backdrop clicked', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    const dialog = screen.getByText('M').parentElement!
    const backdrop = dialog.parentElement!
    fireEvent.click(backdrop)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('does not call onCancel when dialog content clicked', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    // Click inside the dialog (message area)
    fireEvent.click(screen.getByText('M'))
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('calls onCancel on Escape key', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('does not call onCancel on other keys', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('removes event listener on unmount', () => {
    const onCancel = vi.fn()
    const { unmount } = render(
      <ConfirmDialog open={true} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    unmount()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('does not attach listener when open is false', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog open={false} title="T" message="M" onConfirm={() => {}} onCancel={onCancel} />
    )
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).not.toHaveBeenCalled()
  })
})
