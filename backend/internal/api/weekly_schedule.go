package api

import "time"

const (
	WeeklyGenerationHourCST = 5
	WeeklyCatchUpWeekCount  = 2
)

type WeeklyGenerationStatus string

const (
	WeeklyGenerationReady      WeeklyGenerationStatus = "ready"
	WeeklyGenerationScheduled  WeeklyGenerationStatus = "scheduled"
	WeeklyGenerationPending    WeeklyGenerationStatus = "pending"
	WeeklyGenerationNotPlanned WeeklyGenerationStatus = "not_planned"
)

type WeeklyGenerationMetadata struct {
	Status                WeeklyGenerationStatus
	Pending               bool
	EstimatedGenerationAt *time.Time
}

func WeeklyScheduledAt(weekStart time.Time) time.Time {
	nextMonday := startOfWeek(weekStart).AddDate(0, 0, 7)
	return time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), WeeklyGenerationHourCST, 0, 0, 0, shanghai)
}

func RecentCompletedWeekStarts(now time.Time, count int) []time.Time {
	thisWeek := startOfWeek(now)
	weeks := make([]time.Time, 0, count)
	for k := 1; k <= count; k++ {
		weeks = append(weeks, thisWeek.AddDate(0, 0, -7*k))
	}
	return weeks
}

func WeeklyGenerationMetadataAt(now, weekStart time.Time, cached bool) WeeklyGenerationMetadata {
	if cached {
		return WeeklyGenerationMetadata{Status: WeeklyGenerationReady}
	}

	weekStart = startOfWeek(weekStart)
	scheduledAt := WeeklyScheduledAt(weekStart)
	if now.In(shanghai).Before(scheduledAt) {
		return WeeklyGenerationMetadata{
			Status:                WeeklyGenerationScheduled,
			Pending:               true,
			EstimatedGenerationAt: &scheduledAt,
		}
	}

	thisWeek := startOfWeek(now)
	oldestEligible := thisWeek.AddDate(0, 0, -7*WeeklyCatchUpWeekCount)
	if !weekStart.Before(oldestEligible) && weekStart.Before(thisWeek) {
		return WeeklyGenerationMetadata{Status: WeeklyGenerationPending, Pending: true}
	}
	return WeeklyGenerationMetadata{Status: WeeklyGenerationNotPlanned}
}
