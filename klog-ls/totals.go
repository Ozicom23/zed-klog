package main

import (
	"time"

	"github.com/jotaen/klog/klog"
	"github.com/jotaen/klog/klog/service"
)

// recordTotal is a record's total time, like `klog total --now` computes it.
type recordTotal struct {
	total klog.Duration
	// running is the time of the open range so far, if it is counted.
	running klog.Duration
	// uncounted is true if the record has an open range that cannot be
	// counted, because it didn't start today or yesterday (or starts later).
	uncounted bool
}

func totalOf(r klog.Record, now time.Time) recordTotal {
	result := recordTotal{total: service.Total(r)}
	if or := r.OpenRange(); or != nil {
		if running, ok := runningTime(r.Date(), or, now); ok {
			result.running = running
			result.total = result.total.Plus(running)
		} else {
			result.uncounted = true
		}
	}
	return result
}

// runningTime returns how long an open range has been running. Like klog, it
// only counts open ranges of today's or yesterday's record.
func runningTime(date klog.Date, or klog.OpenRange, now time.Time) (klog.Duration, bool) {
	today := klog.NewDateFromGo(now)
	end := klog.NewTimeFromGo(now)
	switch {
	case date.IsEqualTo(today):
	case date.IsEqualTo(today.PlusDays(-1)):
		shifted, err := end.Plus(klog.NewDuration(24, 0))
		if err != nil {
			return nil, false
		}
		end = shifted
	default:
		return nil, false
	}
	r, err := klog.NewRange(or.Start(), end)
	if err != nil {
		return nil, false
	}
	return r.Duration(), true
}

// label is the text shown next to the record's date.
func (t recordTotal) label(info *recordInfo) string {
	text := "total " + t.total.ToString()
	if t.running != nil {
		text += " so far"
	}
	if info.hasShouldTotal {
		text += ", diff " + service.Diff(info.record.ShouldTotal(), t.total).ToStringWithSign()
	}
	if t.uncounted {
		text += ", open range not counted"
	}
	return text
}
