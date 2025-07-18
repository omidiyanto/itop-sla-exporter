package utils

import (
	"time"
)

// CalculateBusinessHourDuration calculates duration between two times, only counting work hours and excluding holidays.
func CalculateBusinessHourDuration(start, end time.Time, workStart, workEnd string, holidays map[string]struct{}) time.Duration {
	if end.Before(start) {
		return 0
	}
	// Parse work hours
	ws, _ := time.Parse("15:04", workStart)
	we, _ := time.Parse("15:04", workEnd)
	var total time.Duration
	cur := start
	for cur.Before(end) {
		// Check holiday
		dateStr := cur.Format("2006-01-02")
		if _, isHoliday := holidays[dateStr]; isHoliday {
			cur = time.Date(cur.Year(), cur.Month(), cur.Day()+1, ws.Hour(), ws.Minute(), 0, 0, cur.Location())
			continue
		}
		// Work hour window
		workDayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), ws.Hour(), ws.Minute(), 0, 0, cur.Location())
		workDayEnd := time.Date(cur.Year(), cur.Month(), cur.Day(), we.Hour(), we.Minute(), 0, 0, cur.Location())
		if cur.Before(workDayStart) {
			cur = workDayStart
		}
		if cur.After(workDayEnd) {
			cur = time.Date(cur.Year(), cur.Month(), cur.Day()+1, ws.Hour(), ws.Minute(), 0, 0, cur.Location())
			continue
		}
		// Next step: either end of workday or end
		next := workDayEnd
		if end.Before(workDayEnd) {
			next = end
		}
		total += next.Sub(cur)
		cur = next
		if cur.Before(end) {
			cur = time.Date(cur.Year(), cur.Month(), cur.Day()+1, ws.Hour(), ws.Minute(), 0, 0, cur.Location())
		}
	}
	return total
}
