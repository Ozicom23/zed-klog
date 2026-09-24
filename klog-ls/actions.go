package main

import (
	"strings"
	"time"

	"github.com/jotaen/klog/klog"
	"github.com/jotaen/klog/klog/parser"
	"github.com/jotaen/klog/klog/parser/reconciling"
)

const codeActionKind = "refactor.rewrite"

// codeActions offers what `klog stop`, `klog start` and `klog create` do. They
// use klog's own reconcilers, which edit the file the way the klog CLI would.
// Like the CLI, they are only offered if the whole file is valid.
//
// So that not every line has code actions, they are only offered in the
// record they change, in the latest record, and on lines between records.
func (d *document) codeActions(line int, only []string, now time.Time) []CodeAction {
	result := []CodeAction{}
	if !kindRequested(only, codeActionKind) || len(d.errors) > 0 {
		return result
	}
	add := func(title string, newText string, ok bool) {
		if ok {
			result = append(result, CodeAction{
				Title: title,
				Kind:  codeActionKind,
				Edit:  &WorkspaceEdit{Changes: map[string][]TextEdit{d.uri: {d.minimalEdit(newText)}}},
			})
		}
	}

	cursor := d.recordAt(line)
	near := func(r *recordInfo) bool { return cursor == nil || cursor == r }
	today := klog.NewDateFromGo(now)
	todays := d.recordOn(today)
	latest := d.latestRecord(nil)

	if r := d.stoppableRecord(today); r != nil && near(r) {
		add(d.stop(r, now))
	}
	if todays != nil && near(todays) || todays == nil && near(latest) {
		add(d.start(now))
	}
	if todays == nil && near(latest) {
		add(d.create(now))
	}
	return result
}

// stoppableRecord returns the record whose open range `klog stop` closes:
// today's record, or else yesterday's.
func (d *document) stoppableRecord(today klog.Date) *recordInfo {
	for _, date := range []klog.Date{today, today.PlusDays(-1)} {
		if r := d.recordOn(date); r != nil && r.record.OpenRange() != nil {
			return r
		}
	}
	return nil
}

// stop closes the record's open range at the current time, like `klog stop`.
func (d *document) stop(r *recordInfo, now time.Time) (string, string, bool) {
	end := klog.NewTimeFromGo(now)
	suffix := ""
	if !r.record.Date().IsEqualTo(klog.NewDateFromGo(now)) {
		shifted, err := end.Plus(klog.NewDuration(24, 0))
		if err != nil {
			return "", "", false
		}
		end, suffix = shifted, " (in yesterday's record)"
	}
	openRange := -1
	result, ok := d.reconcile(
		[]reconciling.Creator{reconciling.NewReconcilerAtRecord(r.record.Date())},
		func(rc *reconciling.Reconciler) error {
			openRange = openRangeIndex(rc.Record)
			return rc.CloseOpenRange(end, reconciling.ReformatAutoStyle[klog.TimeFormat](), nil)
		},
	)
	if !ok || openRange < 0 {
		return "", "", false
	}
	// Show the end time as it was written into the file, which follows the
	// file's style (e.g. `2:32pm`).
	entry := result.Record.Entries()[openRange]
	endText := klog.Unbox[string](&entry,
		func(rg klog.Range) string { return strings.TrimSuffix(rg.End().ToString(), ">") },
		func(klog.Duration) string { return "" },
		func(klog.OpenRange) string { return "" },
	)
	return "Stop open range at " + endText + suffix, result.AllSerialised, true
}

// start adds an open range at the current time to today's record, creating
// the record if needed, like `klog start`.
func (d *document) start(now time.Time) (string, string, bool) {
	today := klog.NewDateFromGo(now)
	result, ok := d.reconcile(
		[]reconciling.Creator{
			reconciling.NewReconcilerAtRecord(today),
			reconciling.NewReconcilerForNewRecord(today, reconciling.ReformatAutoStyle[klog.DateFormat](), d.newRecordData()),
		},
		func(rc *reconciling.Reconciler) error {
			return rc.StartOpenRange(klog.NewTimeFromGo(now), reconciling.ReformatAutoStyle[klog.TimeFormat](), nil)
		},
	)
	if !ok || result.Record.OpenRange() == nil {
		return "", "", false
	}
	title := "Start open range at " + result.Record.OpenRange().Start().ToString()
	if d.recordOn(today) == nil {
		title += " in a new record for today"
	}
	return title, result.AllSerialised, true
}

// create adds a record for today, like `klog create`.
func (d *document) create(now time.Time) (string, string, bool) {
	data := d.newRecordData()
	result, ok := d.reconcile(
		[]reconciling.Creator{
			reconciling.NewReconcilerForNewRecord(klog.NewDateFromGo(now), reconciling.ReformatAutoStyle[klog.DateFormat](), data),
		},
		func(*reconciling.Reconciler) error { return nil },
	)
	if !ok {
		return "", "", false
	}
	header := result.Record.Date().ToString()
	if data.ShouldTotal != nil {
		header += " (" + result.Record.ShouldTotal().ToString() + ")"
	}
	return "Add record for today: " + header, result.AllSerialised, true
}

// reconcile runs the first creator that finds its record, the same way the
// klog CLI does, and returns the result.
func (d *document) reconcile(creators []reconciling.Creator, apply func(*reconciling.Reconciler) error) (*reconciling.Result, bool) {
	// Parse again, because the reconcilers modify the records.
	records, blocks, errs := parser.NewSerialParser().Parse(d.text)
	if errs != nil {
		return nil, false
	}
	for _, create := range creators {
		r := create(records, blocks)
		if r == nil {
			continue
		}
		if err := apply(r); err != nil {
			return nil, false
		}
		result, err := r.MakeResult()
		if err != nil {
			return nil, false
		}
		return result, true
	}
	return nil, false
}

// newRecordData returns what new records start with. The klog CLI takes the
// should-total from its configuration; here it comes from the latest record.
func (d *document) newRecordData() reconciling.AdditionalData {
	latest := d.latestRecord(func(r *recordInfo) bool { return r.hasShouldTotal })
	if latest == nil {
		return reconciling.AdditionalData{}
	}
	return reconciling.AdditionalData{ShouldTotal: latest.record.ShouldTotal()}
}

// latestRecord returns the record with the latest date among those that
// `match` accepts (all of them if `match` is nil).
func (d *document) latestRecord(match func(*recordInfo) bool) *recordInfo {
	var latest *recordInfo
	for _, r := range d.records {
		if match != nil && !match(r) {
			continue
		}
		if latest == nil || r.record.Date().IsAfterOrEqual(latest.record.Date()) {
			latest = r
		}
	}
	return latest
}

// recordOn returns the first record with the given date, which is the one
// that klog's reconcilers edit.
func (d *document) recordOn(date klog.Date) *recordInfo {
	for _, r := range d.records {
		if r.record.Date().IsEqualTo(date) {
			return r
		}
	}
	return nil
}

func openRangeIndex(r klog.Record) int {
	for i, e := range r.Entries() {
		isOpen := klog.Unbox[bool](&e,
			func(klog.Range) bool { return false },
			func(klog.Duration) bool { return false },
			func(klog.OpenRange) bool { return true },
		)
		if isOpen {
			return i
		}
	}
	return -1
}

// kindRequested checks whether the client asked for code actions of this kind.
// Clients ask for specific kinds when running code actions on save, which
// should never start or stop time tracking.
func kindRequested(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	for _, o := range only {
		if kind == o || strings.HasPrefix(kind, o+".") {
			return true
		}
	}
	return false
}
