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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Gauge, RefreshCw, Search, Settings2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { PublicLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimeStr } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getModelMonitor, updateModelMonitorModels } from './api'
import { ManageModelsDialog } from './components/manage-models-dialog'
import { ModelMonitorCard } from './components/model-monitor-card'
import {
  MODEL_MONITOR_WINDOWS,
  type ModelMonitorModel,
  type ModelMonitorWindow,
} from './types'

export function ModelMonitor() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const [minutes, setMinutes] = useState<ModelMonitorWindow>(60)
  const [manageOpen, setManageOpen] = useState(false)
  const [search, setSearch] = useState('')
  const isAdmin = (userRole ?? ROLE.GUEST) >= ROLE.ADMIN

  const monitorQuery = useQuery({
    queryKey: ['model-monitor', minutes],
    queryFn: async () => requireServerSuccess(await getModelMonitor(minutes)),
    staleTime: 10_000,
    refetchInterval: 15_000,
    retry: false,
  })
  const saveMutation = useMutation({
    mutationFn: async (models: ModelMonitorModel[]) =>
      requireServerSuccess(await updateModelMonitorModels(models)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['model-monitor'] })
      setManageOpen(false)
      toast.success(t('Model monitor updated'))
    },
    onError: (error) => handleServerError(error),
  })

  const data = monitorQuery.data?.data
  const models = useMemo(() => data?.models ?? [], [data?.models])
  const metricMap = useMemo(
    () =>
      new Map(
        (data?.metrics ?? []).map((metric) => [metric.model_name, metric])
      ),
    [data?.metrics]
  )
  const summary = useMemo(() => {
    const metrics = data?.metrics ?? []
    const totalCalls = metrics.reduce(
      (total, metric) => total + metric.request_count,
      0
    )
    const successfulCalls = metrics.reduce(
      (total, metric) => total + metric.success_count,
      0
    )
    const failedCalls = Math.max(0, totalCalls - successfulCalls)
    const successRate =
      totalCalls > 0 ? (successfulCalls / totalCalls) * 100 : Number.NaN
    return { totalCalls, successfulCalls, failedCalls, successRate }
  }, [data?.metrics])
  const filteredModels = useMemo(() => {
    const query = search.trim().toLowerCase()
    return models.filter((model) => {
      const description = model.description || ''
      if (
        query &&
        !description.toLowerCase().includes(query) &&
        !model.model_name.toLowerCase().includes(query)
      ) {
        return false
      }
      return true
    })
  }, [models, search])

  let content
  if (monitorQuery.isLoading) {
    content = <LoadingState message={t('Loading model monitor...')} />
  } else if (monitorQuery.isError) {
    content = (
      <ErrorState
        title={t('Failed to load model monitor')}
        description={t('Check the monitoring configuration and try again.')}
        onRetry={() => void monitorQuery.refetch()}
      />
    )
  } else if (models.length === 0) {
    content = (
      <EmptyState
        icon={Gauge}
        title={t('Model monitoring is not configured')}
        description={t(
          'Administrators can add models from the model square to start public call monitoring.'
        )}
        action={
          isAdmin ? (
            <Button onClick={() => setManageOpen(true)}>
              <Settings2 />
              {t('Manage models')}
            </Button>
          ) : undefined
        }
        bordered
      />
    )
  } else if (filteredModels.length === 0) {
    content = (
      <EmptyState
        icon={Search}
        title={t('No matching models')}
        description={t('Adjust the search to see more models.')}
        bordered
      />
    )
  } else {
    content = (
      <div className='grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-4'>
        {filteredModels.map((model) => (
          <ModelMonitorCard
            key={model.model_name}
            modelName={model.model_name}
            description={model.description}
            metric={metricMap.get(model.model_name)}
            minutes={minutes}
            windowStart={data?.window_start}
          />
        ))}
      </div>
    )
  }

  return (
    <>
      <PublicLayout showMainContainer={false}>
        <PageTransition className='mx-auto w-full max-w-[1380px] px-4 pt-16 pb-8 sm:px-6 xl:px-8'>
          <header className='flex flex-col gap-3 border-b pb-4'>
            <div className='flex flex-wrap items-start justify-between gap-4'>
              <div className='max-w-2xl'>
                <div className='text-success flex items-center gap-1.5 text-[11px] font-medium tracking-wide uppercase'>
                  <span className='bg-success size-1.5 rounded-full' />
                  {t('Live relay monitoring')}
                </div>
                <h1 className='mt-1.5 text-2xl font-bold tracking-tight sm:text-3xl'>
                  {t('Model Monitor')}
                </h1>
                <p className='text-muted-foreground mt-2 text-xs leading-5 sm:text-sm'>
                  {t(
                    'Track model request success and failure status around the clock.'
                  )}
                </p>
              </div>

              <div className='flex items-center gap-2'>
                {monitorQuery.isFetching && !monitorQuery.isLoading && (
                  <RefreshCw
                    className='text-muted-foreground size-4 animate-spin'
                    aria-label={t('Refreshing')}
                  />
                )}
                {isAdmin && (
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setManageOpen(true)}
                  >
                    <Settings2 />
                    {t('Manage models')}
                  </Button>
                )}
              </div>
            </div>

            <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-2'>
              <div className='contents sm:flex sm:items-center sm:gap-x-4'>
                <Select
                  value={String(minutes)}
                  onValueChange={(value) => {
                    const next = Number(value)
                    if (
                      MODEL_MONITOR_WINDOWS.includes(next as ModelMonitorWindow)
                    ) {
                      setMinutes(next as ModelMonitorWindow)
                    }
                  }}
                >
                  <SelectTrigger
                    size='sm'
                    className='order-1 w-[96px] text-xs'
                    aria-label={t('Monitoring time window')}
                  >
                    <SelectValue>
                      {t('{{hours}}h', { hours: minutes / 60 })}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {MODEL_MONITOR_WINDOWS.map((window) => (
                        <SelectItem key={window} value={String(window)}>
                          {t('{{hours}}h', { hours: window / 60 })}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <div className='text-muted-foreground order-3 flex basis-full flex-wrap items-center gap-x-3 gap-y-1 text-[11px] sm:basis-auto'>
                  <span className='text-success'>
                    {t('Successful requests')}{' '}
                    <strong className='font-semibold tabular-nums'>
                      {formatNumber(summary.successfulCalls, locale)}
                    </strong>
                  </span>
                  <span className='text-destructive'>
                    {t('Failed requests')}{' '}
                    <strong className='font-semibold tabular-nums'>
                      {formatNumber(summary.failedCalls, locale)}
                    </strong>
                  </span>
                  <span className='text-success'>
                    {t('Success rate')}{' '}
                    <strong className='font-semibold tabular-nums'>
                      {Number.isFinite(summary.successRate)
                        ? `${summary.successRate.toFixed(2)}%`
                        : '—'}
                    </strong>
                  </span>
                </div>
              </div>
              <span className='text-muted-foreground order-2 text-[11px] sm:order-none'>
                {data?.window_end
                  ? t('Updated at {{time}}', {
                      time: formatTimeStr(new Date(data.window_end * 1000)),
                    })
                  : t('Waiting for data')}
              </span>
            </div>
          </header>

          <main className='mt-3 flex flex-col gap-3'>
            <section className='flex items-center'>
              <div className='relative w-full max-w-[220px]'>
                <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('Search models...')}
                  aria-label={t('Search models')}
                  className='bg-background h-8 rounded-[6px] pl-8 text-xs'
                />
              </div>
            </section>

            {content}
          </main>
        </PageTransition>
      </PublicLayout>

      <ManageModelsDialog
        open={manageOpen}
        onOpenChange={setManageOpen}
        availableModels={data?.available_models ?? []}
        selectedModels={models}
        saving={saveMutation.isPending}
        onSave={(selectedModels) => saveMutation.mutate(selectedModels)}
      />
    </>
  )
}
