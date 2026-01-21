import type { HeatmapStat } from '../services/logs'

export type UsageHeatmapDay = {
	label: string
	dateKey: string
	requests: number
	inputTokens: number
	outputTokens: number
	reasoningTokens: number
	cost: number
	intensity: number
}

export type UsageHeatmapWeek = UsageHeatmapDay[]

export const HEATMAP_ROWS = 8
export const BUCKETS_PER_DAY = 3
export const DEFAULT_HEATMAP_DAYS = 21
const HOURS_PER_BUCKET = 8
const LEVELS = 4

const clampDays = (days?: number) => (days && days > 0 ? Math.floor(days) : DEFAULT_HEATMAP_DAYS)

const intensityForCount = (count: number, maxCount: number) => {
	if (count <= 0 || maxCount <= 0) return 0
	const ratio = count / maxCount
	return Math.min(LEVELS, Math.max(1, Math.ceil(ratio * LEVELS)))
}

const startOfDay = (date: Date) => {
	const start = new Date(date)
	start.setHours(0, 0, 0, 0)
	return start
}

const addDays = (date: Date, days: number) => {
	const result = new Date(date)
	result.setDate(result.getDate() + days)
	return result
}

const formatHourKey = (date: Date) => {
	const year = date.getFullYear()
	const month = `${date.getMonth() + 1}`.padStart(2, '0')
	const day = `${date.getDate()}`.padStart(2, '0')
	const hour = `${date.getHours()}`.padStart(2, '0')
	return `${year}-${month}-${day} ${hour}`
}

const labelForCell = (date: Date) => {
	const month = `${date.getMonth() + 1}`.padStart(2, '0')
	const day = `${date.getDate()}`.padStart(2, '0')
	const hour = `${date.getHours()}`.padStart(2, '0')
	return `${month}-${day} ${hour}`
}

const normalizeStatKey = (value?: string | null) => {
	const trimmed = value?.trim()
	if (!trimmed) return null
	const match = trimmed.match(/^(\d{2})-(\d{2}) (\d{2})$/)
	if (!match) {
		return null
	}
	const [, monthStr, dayStr, hourStr] = match
	const now = new Date()
	const year = now.getFullYear()
	return `${year}-${monthStr}-${dayStr} ${hourStr}`
}

type StatBucket = {
	requests: number
	inputTokens: number
	outputTokens: number
	reasoningTokens: number
	cost: number
}

const emptyBucket = (): StatBucket => ({
	requests: 0,
	inputTokens: 0,
	outputTokens: 0,
	reasoningTokens: 0,
	cost: 0,
})

const buildColumns = (
	days: number,
	statsMap: Map<string, StatBucket>,
	startDay: Date,
	maxCount: number,
) => {
	const columns: UsageHeatmapWeek[] = []
	for (let dayIndex = 0; dayIndex < days; dayIndex++) {
		const dayStart = addDays(startDay, dayIndex)
		for (let bucketIndex = 0; bucketIndex < BUCKETS_PER_DAY; bucketIndex++) {
			const column: UsageHeatmapWeek = []
			for (let rowIndex = 0; rowIndex < HEATMAP_ROWS; rowIndex++) {
				const hour = bucketIndex * HEATMAP_ROWS + rowIndex
				const cellTime = new Date(dayStart)
				cellTime.setHours(hour, 0, 0, 0)
				const key = formatHourKey(cellTime)
				const bucket = statsMap.get(key) ?? emptyBucket()
				column.push({
					label: labelForCell(cellTime),
					dateKey: cellTime.toISOString(),
					requests: bucket.requests,
					inputTokens: bucket.inputTokens,
					outputTokens: bucket.outputTokens,
					reasoningTokens: bucket.reasoningTokens,
					cost: bucket.cost,
					intensity: intensityForCount(bucket.requests, maxCount),
				})
			}
			columns.push(column)
		}
	}
	return columns
}

export const generateFallbackUsageHeatmap = (
	days = DEFAULT_HEATMAP_DAYS,
): UsageHeatmapWeek[] => {
	const normalizedDays = clampDays(days)
	const startDay = addDays(startOfDay(new Date()), -(normalizedDays - 1))
	const statsMap = new Map<string, StatBucket>()
	return buildColumns(normalizedDays, statsMap, startDay, 0)
}

export const buildUsageHeatmapMatrix = (
	stats: HeatmapStat[] = [],
	days = DEFAULT_HEATMAP_DAYS,
): UsageHeatmapWeek[] => {
	const normalizedDays = clampDays(days)
	const startDay = addDays(startOfDay(new Date()), -(normalizedDays - 1))
	const statsMap = new Map<string, StatBucket>()

	stats.forEach((stat) => {
		if (!stat) return
		const key = normalizeStatKey(stat.day)
		if (!key) return
		const bucket = statsMap.get(key)
		const update: StatBucket = {
			requests: Number(stat.total_requests) || 0,
			inputTokens: Number(stat.input_tokens) || 0,
			outputTokens: Number(stat.output_tokens) || 0,
			reasoningTokens: Number(stat.reasoning_tokens) || 0,
			cost: Number(stat.total_cost) || 0,
		}
		if (bucket) {
			bucket.requests += update.requests
			bucket.inputTokens += update.inputTokens
			bucket.outputTokens += update.outputTokens
			bucket.reasoningTokens += update.reasoningTokens
			bucket.cost += update.cost
		} else {
			statsMap.set(key, { ...update })
		}
	})

	let maxCount = 0
	statsMap.forEach((bucket) => {
		if (bucket.requests > maxCount) {
			maxCount = bucket.requests
		}
	})

	return buildColumns(normalizedDays, statsMap, startDay, maxCount)
}

export const calculateHeatmapDayRange = (days = DEFAULT_HEATMAP_DAYS) => {
	return clampDays(days)
}
