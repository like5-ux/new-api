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
import { Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

import { MODEL_MONITOR_MAX_MODELS, type ModelMonitorModel } from '../types'

export interface ManageModelsDialogProps {
  open: boolean
  availableModels: string[]
  selectedModels: ModelMonitorModel[]
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (models: ModelMonitorModel[]) => void
}

export function ManageModelsDialog(props: ManageModelsDialogProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [descriptions, setDescriptions] = useState<Record<string, string>>({})
  const selectedModelsRef = useRef(props.selectedModels)

  useEffect(() => {
    selectedModelsRef.current = props.selectedModels
  }, [props.selectedModels])

  useEffect(() => {
    if (!props.open) return
    setQuery('')
    setSelected(
      new Set(selectedModelsRef.current.map((model) => model.model_name))
    )
    setDescriptions(
      Object.fromEntries(
        selectedModelsRef.current.map((model) => [
          model.model_name,
          model.description || '',
        ])
      )
    )
  }, [props.open])

  const filteredModels = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase()
    if (!normalizedQuery) return props.availableModels
    return props.availableModels.filter((modelName) =>
      modelName.toLowerCase().includes(normalizedQuery)
    )
  }, [props.availableModels, query])

  const toggleModel = (modelName: string, checked: boolean) => {
    if (checked) {
      setDescriptions((names) => ({
        ...names,
        [modelName]: names[modelName] || '',
      }))
    }
    setSelected((current) => {
      const next = new Set(current)
      if (checked) {
        if (next.size >= MODEL_MONITOR_MAX_MODELS && !next.has(modelName)) {
          return current
        }
        next.add(modelName)
      } else {
        next.delete(modelName)
      }
      return next
    })
  }

  const handleSave = () => {
    const models = props.availableModels
      .filter((modelName) => selected.has(modelName))
      .map((modelName) => ({
        model_name: modelName,
        description: descriptions[modelName]?.trim() || '',
      }))
    props.onSave(models)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Manage monitored models')}
      description={t(
        'Choose models from the model square to display on the monitoring dashboard.'
      )}
      contentHeight='min(68dvh, 640px)'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' disabled={props.saving} onClick={handleSave}>
            {props.saving && <Spinner data-icon='inline-start' />}
            {props.saving ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <div className='flex h-full min-h-0 flex-col gap-3'>
        <div className='relative'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('Search models...')}
            aria-label={t('Search models')}
            className='pl-8'
          />
        </div>

        <div className='text-muted-foreground flex items-center justify-between gap-3 text-xs'>
          <span>
            {t('{{selected}} of {{total}} selected', {
              selected: selected.size,
              total: props.availableModels.length,
            })}
          </span>
          <span>
            {t('Maximum {{count}} models', {
              count: MODEL_MONITOR_MAX_MODELS,
            })}
          </span>
        </div>

        <div
          role='group'
          aria-label={t('Models available in the model square')}
          className='min-h-0 flex-1 overflow-y-auto rounded-lg border'
        >
          {filteredModels.length > 0 ? (
            filteredModels.map((modelName) => {
              const checked = selected.has(modelName)
              const disabled =
                !checked && selected.size >= MODEL_MONITOR_MAX_MODELS
              const checkboxId = `monitor-model-${encodeURIComponent(modelName)}`
              return (
                <div
                  key={modelName}
                  className='border-b px-3 py-2.5 last:border-b-0'
                >
                  <div className='flex items-center gap-3'>
                    <Checkbox
                      id={checkboxId}
                      checked={checked}
                      disabled={disabled}
                      onCheckedChange={(value) =>
                        toggleModel(modelName, value === true)
                      }
                    />
                    <Label
                      htmlFor={checkboxId}
                      className='min-w-0 flex-1 cursor-pointer truncate font-mono text-sm font-normal'
                      title={modelName}
                    >
                      {modelName}
                    </Label>
                  </div>
                  {checked && (
                    <div className='mt-2 flex items-start gap-2 pl-7'>
                      <Label
                        htmlFor={`${checkboxId}-description`}
                        className='text-muted-foreground w-20 shrink-0 text-xs font-normal'
                      >
                        {t('Model description')}
                      </Label>
                      <Textarea
                        id={`${checkboxId}-description`}
                        value={descriptions[modelName] ?? ''}
                        maxLength={300}
                        rows={2}
                        onChange={(event) =>
                          setDescriptions((names) => ({
                            ...names,
                            [modelName]: event.target.value,
                          }))
                        }
                        aria-label={t('Model description for {{model}}', {
                          model: modelName,
                        })}
                        className='min-h-16 text-xs'
                      />
                    </div>
                  )}
                </div>
              )
            })
          ) : (
            <p className='text-muted-foreground p-6 text-center text-sm'>
              {t('No matching models')}
            </p>
          )}
        </div>
      </div>
    </Dialog>
  )
}
