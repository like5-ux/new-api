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
export const MODEL_MONITOR_WINDOWS = [60, 360, 720, 1440] as const
export const MODEL_MONITOR_MAX_MODELS = 50

export type ModelMonitorWindow = (typeof MODEL_MONITOR_WINDOWS)[number]

export type ModelMonitorModel = {
  model_name: string
  description: string
}

export type ModelMonitorPoint = {
  ts: number
  request_count: number
  success_count: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}

export type ModelMonitorMetric = {
  model_name: string
  request_count: number
  success_count: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  series?: ModelMonitorPoint[]
}

export type ModelMonitorData = {
  models: ModelMonitorModel[]
  available_models: string[]
  metrics: ModelMonitorMetric[]
  window_start: number
  window_end: number
}

export type ModelMonitorResponse = {
  success: boolean
  message?: string
  data: ModelMonitorData
}

export type ModelMonitorModelsResponse = {
  success: boolean
  message?: string
  data: ModelMonitorModel[]
}
