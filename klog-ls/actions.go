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
func (d *document) codeActions(only []string, now time.Time) []CodeAction {
	result := []CodeAction{}
	if !kindRequested(only, codeActionKind) || len(d.errors) > 0 {
		return result
	}
	for _, action := range []func(time.Time) (string, string, bool){d.stop, d.start, d.create} {
		title, newText, ok := action(now)
		if !ok {
			continue
		}
		result = append(result, CodeAction{
			Title: title,
			Kind:  codeActionKind,
			Edit:  &WorkspaceEdit{Changes: map[string][]TextEdit{d.uri: {d.minimalEdit(newText)}}},
		})
	}
	return result
}

// stop closes the open range of today's record (or yesterday's) at the
// current time, like `klog stop`.
func (d *document) stop(now time.Time) (string, string, bool) {
	today := klog.NewDateFromGo(now)
	end := klog.NewTimeFromGo(now)
	for _, date := range []klog.Date{today, today.PlusDays(-1)} {
		if !d.hasOpenRangeOn(date) {
			continue
		}
		endTime, title := end, "Stop open range at "+end.ToString()
		if !date.IsEqualTo(today) {
			shifted, err := end.Plus(klog.NewDuration(24, 0))
			if err != nil {
				continue
			}
			endTime, title = shifted, title+" (in yesterday's record)"
		}
		text, ok := d.reconcile(
			[]reconciling.Creator{reconciling.NewReconcilerAtRecord(date)},
			func(r *reconciling.Reconciler) error {
				return r.CloseOpenRange(endTime, reconciling.ReformatAutoStyle[klog.TimeFormat](), nil)
			},
		)
		if ok {
			return title, text, true
		}
	}
	return "", "", false
}

// start adds an open range at the current time to today's record, creating
// the record if needed, like `klog start`.
func (d *document) start(now time.Time) (string, string, bool) {
	today := klog.NewDateFromGo(now)
	startTime := klog.NewTimeFromGo(now)
	title := "Start open range at " + startTime.ToString()
	if !d.hasRecordOn(today) {
		title += " in a new record for today"
	}
	text, ok := d.reconcile(
		[]reconciling.Creator{
			reconciling.NewReconcilerAtRecord(today),
			reconciling.NewReconcilerForNewRecord(today, reconciling.ReformatAutoStyle[klog.DateFormat](), d.newRecordData()),
		},
		func(r *reconciling.Reconciler) error {
			return r.StartOpenRange(startTime, reconciling.ReformatAutoStyle[klog.TimeFormat](), nil)
		},
	)
	return title, text, ok
}

// create adds a record for today, like `klog create`.
func (d *document) create(now time.Time) (string, string, bool) {
	today := klog.NewDateFromGo(now)
	if d.hasRecordOn(today) {
		return "", "", false
	}
	data := d.newRecordData()
	header := today.ToString()
	if latest := d.latestRecord(); latest != nil {
		header = today.ToStringWithFormat(latest.record.Date().Format())
	}
	if data.ShouldTotal != nil {
		header += " (" + data.ShouldTotal.ToString() + ")"
	}
	text, ok := d.reconcile(
		[]reconciling.Creator{
			reconciling.NewReconcilerForNewRecord(today, reconciling.ReformatAutoStyle[klog.DateFormat](), data),
		},
		func(*reconciling.Reconciler) error { return nil },
	)
	return "Add record for today: " + header, text, ok
}

// reconcile runs the first creator that finds its record, the same way the
// klog CLI does, and returns the resulting text.
func (d *document) reconcile(creators []reconciling.Creator, apply func(*reconciling.Reconciler) error) (string, bool) {
	// Parse again, because the reconcilers modify the records.
	records, blocks, errs := parser.NewSerialParser().Parse(d.text)
	if errs != nil {
		return "", false
	}
	for _, create := range creators {
		r := create(records, blocks)
		if r == nil {
			continue
		}
		if err := apply(r); err != nil {
			return "", false
		}
		result, err := r.MakeResult()
		if err != nil {
			return "", false
		}
		return result.AllSerialised, true
	}
	return "", false
}

// newRecordData returns what new records start with. The klog CLI takes the
// should-total from its configuration; here it comes from the latest record.
func (d *document) newRecordData() reconciling.AdditionalData {
	var latest *recordInfo
	for _, r := range d.records {
		if r.hasShouldTotal && (latest == nil || r.record.Date().IsAfterOrEqual(latest.record.Date())) {
			latest = r
		}
	}
	if latest == nil {
		return reconciling.AdditionalData{}
	}
	return reconciling.AdditionalData{ShouldTotal: latest.record.ShouldTotal()}
}

func (d *document) latestRecord() *recordInfo {
	var latest *recordInfo
	for _, r := range d.records {
		if latest == nil || r.record.Date().IsAfterOrEqual(latest.record.Date()) {
			latest = r
		}
	}
	return latest
}

func (d *document) hasRecordOn(date klog.Date) bool {
	for _, r := range d.records {
		if r.record.Date().IsEqualTo(date) {
			return true
		}
	}
	return false
}

func (d *document) hasOpenRangeOn(date klog.Date) bool {
	for _, r := range d.records {
		if r.record.Date().IsEqualTo(date) && r.record.OpenRange() != nil {
			return true
		}
	}
	return false
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
