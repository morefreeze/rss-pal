// Some feeds serialize absent publication dates as Go's year-one timestamp.
// Validate before locale conversion, which otherwise displays a plausible
// historical date and even a timezone-dependent time of day.
export function parsePublicationDate(value?: string | null): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? null : date
}
