package main

import (
	"strings"
	"time"

	"github.com/jotaen/klog/klog/service"
)

// diagnostics returns klog's parser errors, plus the warnings that the klog CLI
// prints for valid records (e.g. unclosed open ranges or overlapping ranges).
func (d *document) diagnostics(now time.Time) []Diagnostic {
	result := []Diagnostic{}
	for _, e := range d.errors {
		line := d.line(e.line)
		start := runesToUTF16(line, e.start)
		end := runesToUTF16(line, e.start+e.length)
		if end <= start {
			end = utf16Len(line)
		}
		result = append(result, Diagnostic{
			Range:    Range{Start: Position{e.line, start}, End: Position{e.line, end}},
			Severity: SeverityError,
			Code:     e.code,
			Source:   "klog",
			Message:  e.title + ": " + e.details,
		})
	}

	// klog reports warnings per date, as "<date>: <message>".
	warnings := service.CheckForWarnings(now, d.klogRecords(), service.NewDisabledCheckers())
	for _, w := range warnings {
		date, message, ok := strings.Cut(w, ": ")
		if !ok {
			continue
		}
		for _, r := range d.records {
			if r.record.Date().ToString() != date {
				continue
			}
			result = append(result, Diagnostic{
				Range: Range{
					Start: Position{r.headerLine, 0},
					End:   Position{r.headerLine, utf16Len(date)},
				},
				Severity: SeverityWarning,
				Source:   "klog",
				Message:  message,
			})
		}
	}
	return result
}
