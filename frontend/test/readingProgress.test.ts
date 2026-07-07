import {
  computeViewportProgress,
  deriveHistoricalHighWater,
  deriveProgressDisplay,
  evaluateReadingProgress,
} from '../src/utils/readingProgress'

function assertEqual<T>(actual: T, expected: T, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`)
  }
}

function assertClose(actual: number, expected: number, label: string) {
  if (Math.abs(actual - expected) > 0.000001) {
    throw new Error(`${label}: expected ${expected}, got ${actual}`)
  }
}

assertClose(computeViewportProgress(250, 1500, 500), 0.25, 'viewport progress')
assertClose(computeViewportProgress(-50, 1500, 500), 0, 'viewport clamps low')
assertClose(computeViewportProgress(2000, 1500, 500), 1, 'viewport clamps high')

const belowSaved = evaluateReadingProgress({
  currentPosition: 0.2,
  savedHighWater: 0.6,
  activeReadSeconds: 0,
  readingMinutes: 4,
})
assertClose(belowSaved.currentPosition, 0.2, 'below saved current')
assertClose(belowSaved.highWaterPosition, 0.6, 'below saved high-water')
assertEqual(belowSaved.shouldPersist, false, 'below saved does not persist')
assertEqual(belowSaved.isCompleted, false, 'below saved not completed')

const beyondSaved = evaluateReadingProgress({
  currentPosition: 0.72,
  savedHighWater: 0.6,
  activeReadSeconds: 0,
  readingMinutes: 4,
})
assertClose(beyondSaved.currentPosition, 0.72, 'beyond saved current')
assertClose(beyondSaved.highWaterPosition, 0.72, 'beyond saved high-water')
assertEqual(beyondSaved.shouldPersist, true, 'beyond saved persists')

const bottom = evaluateReadingProgress({
  currentPosition: 0.96,
  savedHighWater: 0.7,
  activeReadSeconds: 0,
  readingMinutes: 10,
})
assertEqual(bottom.isCompleted, true, 'bottom scroll completes')

const gated = evaluateReadingProgress({
  currentPosition: 0.91,
  savedHighWater: 0.7,
  activeReadSeconds: 15,
  readingMinutes: 10,
})
assertEqual(gated.isCompleted, true, 'time gate completes')

const displayBeforeLayoutShift = deriveProgressDisplay({
  currentPosition: 0.25,
  historicalHighWater: 0.6,
})
assertClose(displayBeforeLayoutShift.currentPosition, 0.25, 'display current before layout shift')
assertClose(displayBeforeLayoutShift.historicalPosition, 0.6, 'display history before layout shift')
assertEqual(displayBeforeLayoutShift.currentPercent, 25, 'display current percent')
assertEqual(displayBeforeLayoutShift.historicalPercent, 60, 'display history percent')

const displayAfterLayoutShift = deriveProgressDisplay({
  currentPosition: 0.18,
  historicalHighWater: 0.6,
})
assertClose(displayAfterLayoutShift.currentPosition, 0.18, 'display current after layout shift')
assertClose(displayAfterLayoutShift.historicalPosition, 0.6, 'display history remains stable after layout shift')
assertEqual(displayAfterLayoutShift.currentPercent, 18, 'display current percent after layout shift')
assertEqual(displayAfterLayoutShift.historicalPercent, 60, 'display history percent after layout shift')

assertClose(
  deriveHistoricalHighWater(0.6, 0.72),
  0.72,
  'display history does not regress behind local high-water',
)
assertClose(
  deriveHistoricalHighWater(0.8, 0.72),
  0.8,
  'display history accepts newer server high-water',
)

console.log('readingProgress tests passed')
