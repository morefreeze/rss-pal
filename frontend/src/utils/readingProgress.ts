export interface ReadingProgressInput {
  currentPosition: number
  savedHighWater: number
  activeReadSeconds: number
  readingMinutes?: number
}

export interface ReadingProgressResult {
  currentPosition: number
  highWaterPosition: number
  shouldPersist: boolean
  isCompleted: boolean
}

export interface ProgressDisplayInput {
  currentPosition: number
  historicalHighWater: number
}

export interface ProgressDisplay {
  currentPosition: number
  historicalPosition: number
  currentPercent: number
  historicalPercent: number
}

export function clampProgress(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(1, Math.max(0, value))
}

function progressPercent(value: number): number {
  return Math.min(100, Math.round(clampProgress(value) * 100))
}

export function deriveProgressDisplay(input: ProgressDisplayInput): ProgressDisplay {
  const currentPosition = clampProgress(input.currentPosition)
  const historicalPosition = clampProgress(input.historicalHighWater)
  return {
    currentPosition,
    historicalPosition,
    currentPercent: progressPercent(currentPosition),
    historicalPercent: progressPercent(historicalPosition),
  }
}

export function computeViewportProgress(
  scrollTop: number,
  contentScrollHeight: number,
  viewportHeight: number,
): number {
  const scrollableHeight = contentScrollHeight - viewportHeight
  if (scrollableHeight <= 0) return 0
  return clampProgress(scrollTop / scrollableHeight)
}

export function evaluateReadingProgress(input: ReadingProgressInput): ReadingProgressResult {
  const currentPosition = clampProgress(input.currentPosition)
  const savedHighWater = clampProgress(input.savedHighWater)
  const highWaterPosition = Math.max(savedHighWater, currentPosition)
  const readMinutes = input.readingMinutes && input.readingMinutes > 0 ? input.readingMinutes : 1
  const minSeconds = Math.min(15, Math.floor(readMinutes * 30))
  const isCompleted = currentPosition > 0.95 ||
    (currentPosition > 0.9 && input.activeReadSeconds >= minSeconds)

  return {
    currentPosition,
    highWaterPosition,
    shouldPersist: currentPosition > savedHighWater,
    isCompleted,
  }
}
