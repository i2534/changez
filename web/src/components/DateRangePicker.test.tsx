import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import DateRangePicker from './DateRangePicker'

describe('DateRangePicker', () => {
  it('renders From/To labels and date buttons', () => {
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={() => {}} locale="en" />
    )
    expect(screen.getByText('From')).toBeTruthy()
    expect(screen.getByText('To')).toBeTruthy()
    expect(screen.getByText('2026-06-01')).toBeTruthy()
    expect(screen.getByText('2026-06-12')).toBeTruthy()
  })

  it('shows zh labels when locale is zh', () => {
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={() => {}} locale="zh" />
    )
    expect(screen.getByText('从')).toBeTruthy()
    expect(screen.getByText('到')).toBeTruthy()
  })

  it('shows placeholder when since is empty', () => {
    render(
      <DateRangePicker since="" until="2026-06-12" onSinceChange={() => {}} locale="en" />
    )
    expect(screen.getByText('Pick a date')).toBeTruthy()
  })

  it('shows zh placeholder when locale is zh and since is empty', () => {
    render(
      <DateRangePicker since="" until="2026-06-12" onSinceChange={() => {}} locale="zh" />
    )
    expect(screen.getByText('选择日期')).toBeTruthy()
  })

  it('opens calendar when Since button is clicked', () => {
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={() => {}} locale="en" />
    )
    fireEvent.click(screen.getByText('2026-06-01'))

    // Calendar should be rendered (look for month navigation)
    expect(screen.getByText('June 2026')).toBeTruthy()
  })

  it('closes calendar on second click', () => {
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={() => {}} locale="en" />
    )
    const btn = screen.getByText('2026-06-01')
    fireEvent.click(btn)
    expect(screen.getByText('June 2026')).toBeTruthy()

    fireEvent.click(btn)
    expect(screen.queryByText('June 2026')).toBeNull()
  })

  it('calls onSinceChange when a date is selected in calendar', () => {
    const onSinceChange = vi.fn()
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={onSinceChange} locale="en" />
    )
    fireEvent.click(screen.getByText('2026-06-01'))

    const dayButtons = screen.getAllByRole('button').filter(
      (b) => b.textContent === '10'
    )
    expect(dayButtons.length).toBeGreaterThan(0)
    fireEvent.click(dayButtons[0])
    expect(onSinceChange).toHaveBeenCalledTimes(1)
    expect(onSinceChange).toHaveBeenCalledWith(expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/))
  })

  it('closes calendar after selecting a date', () => {
    const onSinceChange = vi.fn()
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={onSinceChange} locale="en" />
    )
    fireEvent.click(screen.getByText('2026-06-01'))

    const dayButtons = screen.getAllByRole('button').filter(
      (b) => b.textContent === '10'
    )
    fireEvent.click(dayButtons[0])
    expect(screen.queryByText('June 2026')).toBeNull()
  })

  it('closes calendar when clicking outside', () => {
    render(
      <DateRangePicker since="2026-06-01" until="2026-06-12" onSinceChange={() => {}} locale="en" />
    )
    fireEvent.click(screen.getByText('2026-06-01'))
    expect(screen.getByText('June 2026')).toBeTruthy()

    // Click outside the picker (on document body)
    fireEvent.mouseDown(document.body)
    expect(screen.queryByText('June 2026')).toBeNull()
  })
})
