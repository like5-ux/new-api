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
import type { StatusVariant } from '@/components/status-badge'

import type { ModelMonitorMetric, ModelMonitorPoint } from '../types'

export type ModelMonitorStatus =
  | 'healthy'
  | 'degraded'
  | 'failing'
  | 'no-traffic'

export type ModelMonitorStatusInfo = {
  status: ModelMonitorStatus
  label: string
  variant: StatusVariant
}

export function getModelMonitorStatus(
  metric?: ModelMonitorMetric
): ModelMonitorStatusInfo {
  if (!metric) {
    return {
      status: 'no-traffic',
      label: 'No traffic',
      variant: 'neutral',
    }
  }

  let latestPoint: ModelMonitorPoint | undefined
  for (const point of metric.series ?? []) {
    if (
      point.request_count > 0 &&
      (!latestPoint || point.ts > latestPoint.ts)
    ) {
      latestPoint = point
    }
  }
  const requestCount = latestPoint?.request_count ?? metric.request_count
  const successRate = latestPoint?.success_rate ?? metric.success_rate
  if (requestCount <= 0) {
    return {
      status: 'no-traffic',
      label: 'No traffic',
      variant: 'neutral',
    }
  }
  if (successRate >= 87.5) {
    return { status: 'healthy', label: 'Healthy', variant: 'success' }
  }
  if (successRate >= 25) {
    return { status: 'degraded', label: 'Degraded', variant: 'warning' }
  }
  return { status: 'failing', label: 'Failing', variant: 'danger' }
}
