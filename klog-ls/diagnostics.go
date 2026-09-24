package main

import (
	"strings"
	"time"

	"github.com/jotaen/klog/klog"
	"github.com/jotaen/klog/klog/service"
)

// diagnostics returns klog's parser errors, plus the warnings that the klog CLI
// prints for valid records (e.g. unclosed open ranges or overlapping ranges).
func (d *document) diagnostics(now time.Time) []Diagnostic {
	result := []Diagnostic{}
	for _, e := range d.errors {
		line := d.line(e.line)
		lineLength := utf16Len(line)
		start := runesToUTF16(line, e.start)
		end := runesToUTF16(line, e.start+e.length)
		if start >= lineLength {
			// An error at the end of the line, e.g. a missing `)`, would
			// otherwise have an empty range.
			start = 0
		}
		if end <= start {
			end = lineLength
		}
		result = append(result, Diagnostic{
			Range:    Range{Start: Position{e.line, start}, End: Position{e.line, end}},
			Severity: SeverityError,
			Code:     e.code,
			Source:   "klog",
			Message:  e.title + ": " + e.details,
		})
	}
	return append(result, d.warnings(now)...)
}

// warnings checks each record on its own, so that every warning ends up at
// the record it is about. klog's check for unclosed open ranges also depends
// on whether there is a record for today, so an empty record stands in for it.
func (d *document) warnings(now time.Time) []Diagnostic {
	var result []Diagnostic
	today := klog.NewDateFromGo(now)
	hasRecordForToday := d.recordOn(today) != nil
	for _, r := range d.records {
		records := []klog.Record{r.record}
		if hasRecordForToday && !r.record.Date().IsEqualTo(today) {
			records = append(records, klog.NewRecord(today))
		}
		// klog formats warnings as "<date>: <message>".
		date := r.record.Date().ToString()
		for _, w := range service.CheckForWarnings(now, records, service.NewDisabledCheckers()) {
			result = append(result, Diagnostic{
				Range: Range{
					Start: Position{r.headerLine, 0},
					End:   Position{r.headerLine, utf16Len(date)},
				},
				Severity: SeverityWarning,
				Source:   "klog",
				Message:  strings.TrimPrefix(w, date+": "),
			})
		}
	}
	return result
}
