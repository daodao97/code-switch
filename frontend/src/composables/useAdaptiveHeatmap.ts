/**
 * 自适应热力图 Composable
 * @author sm
 * @description 封装热力图自适应逻辑，根据容器宽度动态计算显示的列数
 */
import { ref, computed, type Ref } from 'vue'
import {
	HEATMAP_ROWS,
	BUCKETS_PER_DAY,
	buildUsageHeatmapMatrix,
	generateFallbackUsageHeatmap,
	type UsageHeatmapWeek,
} from '../data/usageHeatmap'
import { fetchHeatmapStats } from '../services/logs'

// 格子尺寸配置（与 CSS 媒体查询保持一致）
const CELL_SIZES = {
	large: { cell: 14, gap: 4, padding: 32 }, // > 960px
	medium: { cell: 12, gap: 3, padding: 24 }, // 640-960px
	small: { cell: 10, gap: 2, padding: 16 }, // < 640px
} as const

// 边界限制
const MIN_COLUMNS = 9 // 最少显示 3 天 (3×3)
const MAX_COLUMNS = 63 // 最多显示 21 天 (21×3)
const MAX_DAYS = 21 // API 请求的最大天数
const DEFAULT_DAYS = 14 // 默认天数

/**
 * 自适应热力图 Composable
 * @param containerRef 热力图容器的 ref 引用
 */
export function useAdaptiveHeatmap(containerRef: Ref<HTMLElement | null>) {
	// 响应式状态
	const containerWidth = ref(0)
	const visibleColumns = ref(DEFAULT_DAYS * BUCKETS_PER_DAY) // 默认 14 天
	const heatmapData = ref<UsageHeatmapWeek[]>(generateFallbackUsageHeatmap(DEFAULT_DAYS))
	const isLoading = ref(false)
	const loadedDays = ref(0) // 已加载的天数

	/**
	 * 获取当前视口下的格子尺寸配置
	 */
	const cellConfig = computed(() => {
		const width = containerWidth.value
		if (width > 960) return CELL_SIZES.large
		if (width > 640) return CELL_SIZES.medium
		return CELL_SIZES.small
	})

	/**
	 * 计算可显示的列数
	 * @param containerWidth 容器宽度
	 */
	const calculateColumns = (containerWidth: number): number => {
		const { cell, gap, padding } = cellConfig.value
		const availableWidth = containerWidth - padding * 2
		const cellUnit = cell + gap

		// 计算可容纳的列数
		const cols = Math.floor((availableWidth + gap) / cellUnit)

		// 应用边界限制
		const bounded = Math.max(MIN_COLUMNS, Math.min(MAX_COLUMNS, cols))

		// 向下取整到 BUCKETS_PER_DAY (3) 的倍数，确保天数完整
		return Math.floor(bounded / BUCKETS_PER_DAY) * BUCKETS_PER_DAY
	}

	/**
	 * 加载热力图数据
	 * @param days 需要加载的天数
	 */
	const loadHeatmapData = async (days: number) => {
		// 如果已加载的天数足够，不重复请求
		if (loadedDays.value >= days && heatmapData.value.length > 0) {
			return
		}

		isLoading.value = true
		try {
			const stats = await fetchHeatmapStats(days)
			heatmapData.value = buildUsageHeatmapMatrix(stats, days)
			loadedDays.value = days
		} catch (error) {
			console.error('Failed to load heatmap data:', error)
		} finally {
			isLoading.value = false
		}
	}

	/**
	 * 节流函数
	 */
	const throttle = <T extends (...args: any[]) => void>(fn: T, delay: number) => {
		let lastCall = 0
		let timeoutId: ReturnType<typeof setTimeout> | null = null
		return (...args: Parameters<T>) => {
			const now = Date.now()
			const remaining = delay - (now - lastCall)
			if (remaining <= 0) {
				if (timeoutId) {
					clearTimeout(timeoutId)
					timeoutId = null
				}
				lastCall = now
				fn(...args)
			} else if (!timeoutId) {
				timeoutId = setTimeout(() => {
					lastCall = Date.now()
					timeoutId = null
					fn(...args)
				}, remaining)
			}
		}
	}

	/**
	 * 处理尺寸变化
	 */
	const handleResize = throttle((width: number) => {
		containerWidth.value = width
		const newColumns = calculateColumns(width)

		// 只有列数变化时才更新
		if (newColumns !== visibleColumns.value) {
			const newDays = Math.min(MAX_DAYS, newColumns / BUCKETS_PER_DAY)
			visibleColumns.value = newColumns

			// 只在需要更多数据时重新请求
			if (newDays > loadedDays.value) {
				void loadHeatmapData(newDays)
			}
		}
	}, 150) // 150ms 节流

	/**
	 * 裁剪显示的数据（只显示最新的 N 列）
	 */
	const displayData = computed(() => {
		const data = heatmapData.value
		if (data.length <= visibleColumns.value) {
			return data
		}
		// 从最新的数据开始显示（数组末尾是最新的）
		return data.slice(data.length - visibleColumns.value)
	})

	// ResizeObserver 实例
	let resizeObserver: ResizeObserver | null = null

	/**
	 * 初始化热力图
	 */
	const init = async () => {
		const container = containerRef.value
		if (!container) return

		// 初始宽度计算
		const initialWidth = container.clientWidth
		containerWidth.value = initialWidth
		const initialColumns = calculateColumns(initialWidth)
		visibleColumns.value = initialColumns

		// 加载初始数据
		const initialDays = Math.min(MAX_DAYS, initialColumns / BUCKETS_PER_DAY)
		await loadHeatmapData(initialDays)

		// 设置 ResizeObserver
		resizeObserver = new ResizeObserver((entries) => {
			for (const entry of entries) {
				const { width } = entry.contentRect
				handleResize(width)
			}
		})
		resizeObserver.observe(container)
	}

	/**
	 * 清理 ResizeObserver
	 */
	const cleanup = () => {
		if (resizeObserver) {
			resizeObserver.disconnect()
			resizeObserver = null
		}
	}

	/**
	 * 重新加载数据
	 */
	const reload = async () => {
		loadedDays.value = 0 // 重置已加载天数，强制重新请求
		const days = Math.min(MAX_DAYS, visibleColumns.value / BUCKETS_PER_DAY)
		await loadHeatmapData(days)
	}

	return {
		containerWidth,
		visibleColumns,
		displayData,
		cellConfig,
		isLoading,
		init,
		cleanup,
		reload,
	}
}
