package explore

import "time"

var exploreShanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

var exploreSlotHours = [...]int{8, 11, 14, 17, 20, 23}

// ExploreSchedule is the canonical worker/API view of one Shanghai day.
// SlotAt is zero before 08:00; NextSlotAt is always populated.
type ExploreSchedule struct {
	HasCurrent         bool
	SlotAt             time.Time
	ProviderSyncAt     time.Time
	NextSlotAt         time.Time
	NextProviderSyncAt time.Time
}

// ExploreScheduleAt returns only the latest slot reached on the current
// Shanghai day. It deliberately never returns an earlier missed slot.
func ExploreScheduleAt(now time.Time) ExploreSchedule {
	local := now.In(exploreShanghaiLocation)
	year, month, day := local.Date()
	result := ExploreSchedule{}
	for index, hour := range exploreSlotHours {
		slot := time.Date(year, month, day, hour, 0, 0, 0, exploreShanghaiLocation)
		if local.Before(slot) {
			result.NextSlotAt = slot
			result.NextProviderSyncAt = slot.Add(-30 * time.Minute)
			break
		}
		result.HasCurrent = true
		result.SlotAt = slot
		result.ProviderSyncAt = slot.Add(-30 * time.Minute)
		if index+1 < len(exploreSlotHours) {
			result.NextSlotAt = time.Date(year, month, day, exploreSlotHours[index+1], 0, 0, 0, exploreShanghaiLocation)
		} else {
			result.NextSlotAt = time.Date(year, month, day+1, exploreSlotHours[0], 0, 0, 0, exploreShanghaiLocation)
		}
		result.NextProviderSyncAt = result.NextSlotAt.Add(-30 * time.Minute)
	}
	return result
}
