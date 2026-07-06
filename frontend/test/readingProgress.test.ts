import {
  computeViewportProgress,
  evaluateReadingProgress,
  rescaleProgressForHeightChange,
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

assertClose(
  rescaleProgressForHeightChange(0.5, 2000, 3000, 1000),
  0.25,
  'height change rescales by scrollable denominator',
)

console.log('readingProgress tests passed')
