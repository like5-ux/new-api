/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ManageModelsDialog } from '../components/manage-models-dialog'
import { ModelMonitorCard } from '../components/model-monitor-card'

describe('model monitor', () => {
  it('filters the model square catalog and saves the selected set', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    render(
      <ManageModelsDialog
        open
        availableModels={['alpha-model', 'beta-model', 'gamma-model']}
        selectedModels={[
          { model_name: 'beta-model', description: '稳定生产线路' },
        ]}
        saving={false}
        onOpenChange={vi.fn()}
        onSave={onSave}
      />
    )

    await user.type(
      screen.getByRole('textbox', { name: 'Search models' }),
      'gamma'
    )
    expect(screen.getByText('gamma-model')).toBeVisible()
    expect(screen.queryByText('alpha-model')).not.toBeInTheDocument()

    await user.click(screen.getByRole('checkbox', { name: 'gamma-model' }))
    const description = screen.getByRole('textbox', {
      name: 'Model description for gamma-model',
    })
    await user.clear(description)
    await user.type(description, '备用线路，延迟略高')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave.mock.calls[0][0]).toEqual([
      { model_name: 'beta-model', description: '稳定生产线路' },
      { model_name: 'gamma-model', description: '备用线路，延迟略高' },
    ])
  })

  it('renders one cell per minute for a 1 hour window', () => {
    const windowStart = Math.floor(Date.now() / 60_000) * 60 - 59 * 60
    render(
      <ModelMonitorCard
        modelName='gpt-test'
        description='生产线路，推荐使用'
        minutes={60}
        windowStart={windowStart}
        metric={{
          model_name: 'gpt-test',
          request_count: 4,
          success_count: 3,
          avg_latency_ms: 1200,
          success_rate: 75,
          avg_tps: 42,
          series: [0, 1, 2, 3].map((offset) => ({
            ts: windowStart + offset * 60,
            request_count: 1,
            success_count: offset === 3 ? 0 : 1,
            avg_latency_ms: 1200,
            success_rate: offset === 3 ? 0 : 100,
            avg_tps: 42,
          })),
        }}
      />
    )

    expect(screen.getByText('gpt-test')).toBeVisible()
    expect(screen.getByText('生产线路，推荐使用')).toBeVisible()
    const history = screen.getByRole('img', {
      name: 'Request history for gpt-test',
    })
    expect(history.children).toHaveLength(60)
    expect(history.children[0].getAttribute('aria-label')).toContain(
      '1 calls, 100.00% success'
    )
    expect(history.children[0].firstElementChild?.className).toContain(
      'bg-success'
    )
    const stats = screen.getByText('Success').closest('dl')
    expect(stats?.className).not.toContain('border')
    expect(stats?.className).not.toContain('rounded-[6px]')
    expect(screen.getByText('Request history')).toBeVisible()
    expect(screen.queryByText('Total calls')).not.toBeInTheDocument()
    expect(history.closest('[data-slot="card"]')?.className).toContain(
      'rounded-[6px]'
    )
    const progress = history.parentElement?.querySelector(
      '[data-slot="request-progress"]'
    )
    expect(progress).toBeInTheDocument()
    expect(progress?.className).toContain('rounded-[6px]')
    expect(progress?.children).toHaveLength(2)
    expect(progress?.children[0].className).toContain('bg-success')
    expect(progress?.children[1].className).toContain('bg-destructive')
  })

  it('keeps progress segments in request history order', () => {
    const windowStart = Math.floor(Date.now() / 60_000) * 60 - 59 * 60
    render(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={60}
        windowStart={windowStart}
        metric={{
          model_name: 'gpt-test',
          request_count: 4,
          success_count: 2,
          avg_latency_ms: 1000,
          success_rate: 50,
          avg_tps: 0,
          series: [0, 1, 2, 3].map((offset) => ({
            ts: windowStart + offset * 60,
            request_count: 1,
            success_count: offset % 2 === 1 ? 1 : 0,
            avg_latency_ms: 1000,
            success_rate: offset % 2 === 1 ? 100 : 0,
            avg_tps: 0,
          })),
        }}
      />
    )

    const history = screen.getByRole('img', {
      name: 'Request history for gpt-test',
    })
    const progress = history.parentElement?.querySelector(
      '[data-slot="request-progress"]'
    )
    expect(progress?.children).toHaveLength(4)
    expect(progress?.children[0].className).toContain('bg-destructive')
    expect(progress?.children[1].className).toContain('bg-success')
    expect(progress?.children[2].className).toContain('bg-destructive')
    expect(progress?.children[3].className).toContain('bg-success')
    expect(screen.getByText('Healthy')).toBeVisible()
  })

  it('uses 30 buckets for longer time windows', () => {
    const windowStart = Math.floor(Date.now() / 60_000) * 60 - 1439 * 60
    const { rerender } = render(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={360}
        windowStart={windowStart}
      />
    )
    expect(
      screen.getByRole('img', { name: 'Request history for gpt-test' }).children
    ).toHaveLength(30)

    rerender(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={720}
        windowStart={windowStart}
      />
    )
    expect(
      screen.getByRole('img', { name: 'Request history for gpt-test' }).children
    ).toHaveLength(30)

    rerender(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={1440}
        windowStart={windowStart}
      />
    )
    expect(
      screen.getByRole('img', { name: 'Request history for gpt-test' }).children
    ).toHaveLength(30)
  })

  it('renders a mixed minute as one degraded bar', () => {
    const windowStart = Math.floor(Date.now() / 60_000) * 60 - 59 * 60
    const { rerender } = render(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={60}
        windowStart={windowStart}
        metric={{
          model_name: 'gpt-test',
          request_count: 7,
          success_count: 5,
          avg_latency_ms: 1200,
          success_rate: 71.43,
          avg_tps: 42,
          series: [
            {
              ts: windowStart,
              request_count: 4,
              success_count: 4,
              avg_latency_ms: 900,
              success_rate: 100,
              avg_tps: 42,
            },
            {
              ts: windowStart + 60,
              request_count: 1,
              success_count: 0,
              avg_latency_ms: 2100,
              success_rate: 0,
              avg_tps: 0,
            },
            {
              ts: windowStart + 2 * 60,
              request_count: 2,
              success_count: 1,
              avg_latency_ms: 1500,
              success_rate: 50,
              avg_tps: 20,
            },
          ],
        }}
      />
    )

    expect(screen.getByText('5')).toBeVisible()
    expect(screen.getByText('2')).toBeVisible()
    expect(screen.getByText('71.43%')).toBeVisible()
    expect(screen.getByText('Degraded')).toBeVisible()
    const history = screen.getByRole('img', {
      name: 'Request history for gpt-test',
    })
    expect(history.children).toHaveLength(60)

    const successfulMinute = history.children[0].firstElementChild
    expect(successfulMinute).toHaveClass('bg-success')
    expect(successfulMinute).toHaveStyle({ height: '100%' })

    const failedMinute = history.children[1].firstElementChild
    expect(failedMinute).toHaveClass('bg-destructive')
    expect(failedMinute).toHaveStyle({ height: '10%' })

    const mixedMinute = history.children[2].firstElementChild
    expect(mixedMinute).toHaveClass('bg-warning')
    expect(mixedMinute).toHaveStyle({ height: '50%' })

    const progress = history.parentElement?.querySelector(
      '[data-slot="request-progress"]'
    )
    expect(progress?.children).toHaveLength(3)
    expect(progress?.children[2].className).toContain('bg-warning')

    rerender(
      <ModelMonitorCard
        modelName='gpt-test'
        minutes={60}
        windowStart={windowStart}
      />
    )
    expect(screen.getByText('No traffic')).toBeVisible()
    expect(screen.getAllByText('0')).toHaveLength(2)

    const emptyHistory = screen.getByRole('img', {
      name: 'Request history for gpt-test',
    })
    const emptyCell = emptyHistory.children[0]
    const emptyDot = emptyCell.firstElementChild
    expect(emptyCell.className).toContain('items-end')
    expect(emptyDot?.className).toContain('size-[3px]')
    expect(emptyDot?.className).not.toContain('h-full')
  })
})
