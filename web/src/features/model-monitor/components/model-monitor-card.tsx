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
import { History } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimeStr } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getModelMonitorStatus } from '../lib/status'
import type { ModelMonitorMetric, ModelMonitorWindow } from '../types'

const VISUAL_BUCKET_COUNTS: Record<ModelMonitorWindow, number> = {
  60: 60,
  360: 30,
  720: 30,
  1440: 30,
}

type MonitorBarBucket = {
  startTs: number
  endTs: number
  requestCount: number
  successCount: number
}

type MonitorBucketStatus = 'healthy' | 'degraded' | 'failing' | 'no-traffic'

const MONITOR_STATUS_COLORS: Record<
  Exclude<MonitorBucketStatus, 'no-traffic'>,
  string
> = {
  healthy: 'bg-success',
  degraded: 'bg-warning',
  failing: 'bg-destructive',
}

export interface ModelMonitorCardProps {
  modelName: string
  description?: string
  metric?: ModelMonitorMetric
  minutes: ModelMonitorWindow
  windowStart?: number
}

export function ModelMonitorCard(props: ModelMonitorCardProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const status = getModelMonitorStatus(props.metric)
  const requestCount = props.metric?.request_count ?? 0
  const successCount = props.metric?.success_count ?? 0
  const failureCount = Math.max(0, requestCount - successCount)
  const hasData = requestCount > 0

  const slots = useMemo(() => {
    const seriesMap = new Map(
      (props.metric?.series ?? []).map((point) => [point.ts, point])
    )
    const firstSeriesTs = props.metric?.series?.[0]?.ts
    const start =
      props.windowStart ??
      firstSeriesTs ??
      Math.floor(Date.now() / 60_000) * 60 - (props.minutes - 1) * 60
    const bucketCount = VISUAL_BUCKET_COUNTS[props.minutes]
    const bucketMinutes = props.minutes / bucketCount
    return Array.from({ length: bucketCount }, (_, index) => {
      const startTs = start + index * bucketMinutes * 60
      const endTs = startTs + (bucketMinutes - 1) * 60
      let requestCount = 0
      let successCount = 0
      for (let offset = 0; offset < bucketMinutes; offset++) {
        const point = seriesMap.get(startTs + offset * 60)
        requestCount += point?.request_count ?? 0
        successCount += point?.success_count ?? 0
      }
      return { startTs, endTs, requestCount, successCount }
    })
  }, [props.metric?.series, props.minutes, props.windowStart])
  const progressSegments = useMemo(() => {
    const segments: Array<{
      status: Exclude<MonitorBucketStatus, 'no-traffic'>
      count: number
      startIndex: number
    }> = []
    for (const [slotIndex, slot] of slots.entries()) {
      const bucketStatus = monitorBucketStatus(slot)
      if (bucketStatus === 'no-traffic') continue
      const previous = segments.at(-1)
      if (previous?.status === bucketStatus) {
        previous.count += slot.requestCount
      } else {
        segments.push({
          status: bucketStatus,
          count: slot.requestCount,
          startIndex: slotIndex,
        })
      }
    }
    return segments.map((segment) => ({
      ...segment,
      colorClassName: MONITOR_STATUS_COLORS[segment.status],
    }))
  }, [slots])

  return (
    <Card className='border-border/70 bg-card group hover:border-primary/25 h-full gap-0 overflow-hidden rounded-[6px] py-0 shadow-[0_1px_2px_rgba(0,0,0,0.03)] transition-[border-color,box-shadow] duration-200 hover:shadow-[0_4px_14px_rgba(0,0,0,0.06)]'>
      <CardHeader className='border-border/60 from-muted/55 via-muted/25 rounded-t-[6px] border-b bg-gradient-to-r to-transparent px-3 py-2.5'>
        <div className='flex min-w-0 items-center justify-between gap-2.5'>
          <div className='min-w-0 flex-1'>
            <CardTitle
              className='text-foreground truncate font-mono text-[13px] font-medium'
              title={props.modelName}
            >
              {props.modelName}
            </CardTitle>
            {props.description?.trim() && (
              <p className='text-muted-foreground mt-0.5 line-clamp-2 text-[10px] leading-4'>
                {props.description}
              </p>
            )}
          </div>
          <StatusBadge
            variant={status.variant}
            size='sm'
            pulse={status.status === 'healthy'}
            className='h-5 shrink-0 px-1.5 text-[11px]'
          >
            {t(status.label)}
          </StatusBadge>
        </div>
      </CardHeader>

      <CardContent className='flex flex-col gap-3 p-3'>
        <dl className='text-foreground grid grid-cols-3 divide-x text-[10px]'>
          <div className='min-w-0 px-2 py-1.5'>
            <dt className='text-muted-foreground font-normal'>
              {t('Success')}
            </dt>
            <dd className='mt-0.5 text-xs leading-4 font-normal tabular-nums'>
              {formatNumber(successCount, locale)}
            </dd>
          </div>
          <div className='min-w-0 px-2 py-1.5'>
            <dt className='text-muted-foreground font-normal'>{t('Failed')}</dt>
            <dd className='mt-0.5 text-xs leading-4 font-normal tabular-nums'>
              {formatNumber(failureCount, locale)}
            </dd>
          </div>
          <div className='min-w-0 px-2 py-1.5'>
            <dt className='text-muted-foreground font-normal'>
              {t('Success rate')}
            </dt>
            <dd className='mt-0.5 text-xs leading-4 font-normal tabular-nums'>
              {hasData ? `${props.metric?.success_rate.toFixed(2)}%` : '—'}
            </dd>
          </div>
        </dl>

        <div className='flex flex-col gap-1.5'>
          <span className='text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium'>
            <History aria-hidden='true' className='size-3' />
            {t('Request history')}
          </span>

          <div className='border-border/60 bg-muted/20 rounded-[6px] border p-1.5'>
            <div
              className='grid h-[21px] gap-[1px]'
              style={{
                gridTemplateColumns: `repeat(${slots.length}, minmax(0, 1fr))`,
              }}
              role='img'
              aria-label={t('Request history for {{model}}', {
                model: props.modelName,
              })}
            >
              {slots.map((slot) => (
                <MonitorBar
                  key={slot.startTs}
                  bucket={slot}
                  label={barLabel(t, slot, props.minutes)}
                />
              ))}
            </div>
            <div
              data-slot='request-progress'
              aria-hidden='true'
              className='bg-muted/70 mt-1.5 flex h-[6px] overflow-hidden rounded-[6px]'
            >
              {progressSegments.map((segment) => (
                <span
                  key={`${segment.status}-${segment.startIndex}`}
                  className={cn('h-full', segment.colorClassName)}
                  style={{ flexBasis: 0, flexGrow: segment.count }}
                />
              ))}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

type TranslationFunction = ReturnType<typeof useTranslation>['t']

function barLabel(
  t: TranslationFunction,
  bucket: MonitorBarBucket,
  minutes: ModelMonitorWindow
): string {
  const bucketMinutes = minutes / VISUAL_BUCKET_COUNTS[minutes]
  const start = formatTimeStr(new Date(bucket.startTs * 1000))
  const time =
    bucketMinutes > 1
      ? `${start}–${formatTimeStr(new Date(bucket.endTs * 1000))}`
      : start
  if (bucket.requestCount <= 0) {
    return t('{{time}}: no calls', { time })
  }
  const rate = (bucket.successCount / bucket.requestCount) * 100
  return t('{{time}}: {{count}} calls, {{rate}} success', {
    time,
    count: bucket.requestCount,
    rate: `${rate.toFixed(2)}%`,
  })
}

function MonitorBar(props: { bucket: MonitorBarBucket; label: string }) {
  const status = monitorBucketStatus(props.bucket)
  const availabilityRate = monitorAvailabilityRate(props.bucket)

  return (
    <span
      title={props.label}
      aria-label={props.label}
      className='flex h-full min-w-0 items-end justify-center'
    >
      {status === 'no-traffic' ? (
        <span className='bg-muted-foreground/30 size-[3px] rounded-full' />
      ) : (
        <span
          className={cn('w-full rounded-[6px]', MONITOR_STATUS_COLORS[status])}
          style={{ height: `${Math.max(10, availabilityRate)}%` }}
        />
      )}
    </span>
  )
}

function monitorAvailabilityRate(bucket: MonitorBarBucket): number {
  if (bucket.requestCount <= 0) return 0
  const successCount = Math.min(bucket.successCount, bucket.requestCount)
  return Math.max(0, Math.min(100, (successCount / bucket.requestCount) * 100))
}

function monitorBucketStatus(bucket: MonitorBarBucket): MonitorBucketStatus {
  if (bucket.requestCount <= 0) {
    return 'no-traffic'
  }
  const successRate = monitorAvailabilityRate(bucket)
  if (successRate >= 87.5) {
    return 'healthy'
  }
  if (successRate >= 25) {
    return 'degraded'
  }
  return 'failing'
}
