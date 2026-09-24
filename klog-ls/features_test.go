package main

import (
	"strings"
	"testing"
	"time"
)

// now is Thursday, 24 September 2026, 14:32.
var now = time.Date(2026, 9, 24, 14, 32, 0, 0, time.Local)

func TestDiagnosticsReportErrorsAndWarnings(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, `2026-09-01
    9:00 - ?

2026-09-02 (8h
    1h
`)
	diagnostics := d.diagnostics(now)
	if len(diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(diagnostics), diagnostics)
	}

	e := diagnostics[0]
	if e.Severity != SeverityError || e.Code != "ErrorMalformedPropertiesSyntax" || e.Range.Start.Line != 3 {
		t.Errorf("unexpected error: %+v", e)
	}
	if !strings.HasPrefix(e.Message, "Malformed should-total time: ") {
		t.Errorf("unexpected error message: %q", e.Message)
	}

	w := diagnostics[1]
	wantRange := Range{Position{0, 0}, Position{0, 10}}
	if w.Severity != SeverityWarning || w.Message != "Unclosed open range" || w.Range != wantRange {
		t.Errorf("unexpected warning: %+v", w)
	}
}

func TestInlayHintsShowTotals(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, `2026-09-20 (8h!)
    2h
    9:00 - 12:30

2026-09-21
    -30m

2026-09-24 (8h!)
    8:00 - 12:00
    13:00 - ?

2026-09-10
    8:00 - ?
`)
	hints := d.inlayHints(Range{Position{0, 0}, Position{100, 0}}, now)
	want := []InlayHint{
		{Position{0, 16}, "total 5h30m, diff -2h30m", true},
		{Position{4, 10}, "total -30m", true},
		{Position{7, 16}, "total 5h32m so far, diff -2h28m", true},
		{Position{11, 10}, "total 0m, open range not counted", true},
	}
	if len(hints) != len(want) {
		t.Fatalf("got %+v", hints)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Errorf("hint %d: got %+v, want %+v", i, hints[i], want[i])
		}
	}

	if got := d.inlayHints(Range{Position{3, 0}, Position{6, 0}}, now); len(got) != 1 {
		t.Errorf("hints are not limited to the requested range: %+v", got)
	}
}

func TestInlayHintsCountYesterdaysOpenRangeAcrossMidnight(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-23\n    22:00 - ?\n")
	hints := d.inlayHints(Range{Position{0, 0}, Position{2, 0}}, time.Date(2026, 9, 24, 0, 45, 0, 0, time.Local))
	if len(hints) != 1 || hints[0].Label != "total 2h45m so far" {
		t.Errorf("got %+v", hints)
	}
}

func TestHover(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, `2026-09-24 (8h!)
Office day #work
    8:30 - 10:30 Layout #project="Big One"
    <23:00 - 1:00 Night shift
    1h #project=other
    13:00 - ? Still going
`)
	tests := []struct {
		name string
		pos  Position
		want string // A substring of the hover text; empty for no hover.
	}{
		{"date line", Position{0, 3}, "**Thursday, 24 September 2026**\n\n- Total: **6h32m** so far\n- Should-total: 8h!\n- Diff: -1h28m\n- Entries: 4\n- Open range since 13:00, running for 1h32m\n"},
		{"range", Position{2, 6}, "**2h** from 8:30 to 10:30"},
		{"shifted range", Position{3, 5}, "**2h** from 23:00 (the day before) to 1:00"},
		{"open range", Position{5, 12}, "Open range since 13:00, running for **1h32m**"},
		{"duration", Position{4, 5}, ""},
		{"summary text", Position{2, 20}, ""},
		{"tag with value", Position{2, 28}, `**#project="Big One"**: 2h in 1 entry` + "\n\n**#project** (any value): 3h in 2 entries"},
		{"tag in record summary", Position{1, 13}, "**#work**: 5h in 4 entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := d.hover(tt.pos, now)
			switch {
			case tt.want == "" && h != nil:
				t.Errorf("got hover %q, want none", h.Contents.Value)
			case tt.want != "" && h == nil:
				t.Errorf("got no hover, want %q", tt.want)
			case tt.want != "" && !strings.Contains(h.Contents.Value, tt.want):
				t.Errorf("got %q, want %q", h.Contents.Value, tt.want)
			}
		})
	}
}

func TestCompletionOfTagNames(t *testing.T) {
	text := "2026-09-24\n    1h #idc #ticket=A-1\n    2h #IDC #meeting #call=\"Liz Jones\"\n    3h #idc #ti"
	d := parseDocument("file:///test.klg", 1, text)
	list := d.completion(Position{3, 15})

	var labels []string
	for _, item := range list.Items {
		labels = append(labels, item.Label)
	}
	if got, want := strings.Join(labels, " "), "#idc #call #meeting #ticket"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	first := list.Items[0]
	wantEdit := TextEdit{Range: Range{Position{3, 12}, Position{3, 15}}, NewText: "#idc"}
	if *first.TextEdit != wantEdit || first.Detail != "3 uses" {
		t.Errorf("unexpected item: %+v, edit %+v", first, *first.TextEdit)
	}

	if got := d.completion(Position{0, 4}); len(got.Items) != 0 {
		t.Errorf("offered completions in the date line: %+v", got.Items)
	}
}

func TestCompletionOfTagValues(t *testing.T) {
	text := "2026-09-24\n    1h #call=\"Liz Jones\" #call=Bob\n    2h #call=\"Liz Jones\"\n    3h #Call=\"L"
	d := parseDocument("file:///test.klg", 1, text)
	list := d.completion(Position{3, 15})
	if len(list.Items) != 2 {
		t.Fatalf("got %+v", list.Items)
	}
	wantEdit := TextEdit{Range: Range{Position{3, 13}, Position{3, 15}}, NewText: `"Liz Jones"`}
	if item := list.Items[0]; item.Label != `"Liz Jones"` || *item.TextEdit != wantEdit {
		t.Errorf("unexpected first item: %+v", item)
	}
	if item := list.Items[1]; item.Label != "Bob" {
		t.Errorf("unexpected second item: %+v", item)
	}
}

func TestCodeActions(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		title string
		want  string // The text after applying the action.
	}{
		{
			name:  "stop",
			text:  "2026-09-24\n    9:00 - ? Work\n",
			title: "Stop open range at 14:32",
			want:  "2026-09-24\n    9:00 - 14:32 Work\n",
		},
		{
			name:  "stop in yesterday's record",
			text:  "2026-09-23\n    22:00-???\n",
			title: "Stop open range at 14:32 (in yesterday's record)",
			want:  "2026-09-23\n    22:00-14:32>\n",
		},
		{
			name:  "start in today's record",
			text:  "2026-09-24\n  8:00 - 9:00\n",
			title: "Start open range at 14:32",
			want:  "2026-09-24\n  8:00 - 9:00\n  14:32 - ?\n",
		},
		{
			name:  "start in a new record",
			text:  "2026/09/20 (8h!)\n\t2h\n",
			title: "Start open range at 14:32 in a new record for today",
			want:  "2026/09/20 (8h!)\n\t2h\n\n2026/09/24 (8h!)\n\t14:32 - ?\n",
		},
		{
			name:  "create",
			text:  "2026-09-20 (7h30m!)\n    2h\n\n2026-09-22\n    1h\n",
			title: "Add record for today: 2026-09-24 (7h30m!)",
			want:  "2026-09-20 (7h30m!)\n    2h\n\n2026-09-22\n    1h\n\n2026-09-24 (7h30m!)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parseDocument("file:///test.klg", 1, tt.text)
			for _, a := range d.codeActions(nil, now) {
				if a.Title != tt.title {
					continue
				}
				if got := applyEdits(d.text, d, a.Edit.Changes[d.uri]); got != tt.want {
					t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
				}
				return
			}
			t.Errorf("no action %q in %+v", tt.title, titles(d.codeActions(nil, now)))
		})
	}
}

func TestCodeActionsAreOnlyOfferedWhenAppropriate(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-24\n    9:00 - ?\n")
	if got := titles(d.codeActions(nil, now)); strings.Join(got, "|") != "Stop open range at 14:32" {
		t.Errorf("got %q; starting another open range or creating today's record should not be offered", got)
	}
	if got := d.codeActions([]string{"source.fixAll"}, now); len(got) != 0 {
		t.Errorf("offered actions for other kinds: %+v", got)
	}
	if got := d.codeActions([]string{"refactor"}, now); len(got) != 1 {
		t.Errorf("did not offer actions for the parent kind: %+v", got)
	}

	invalid := parseDocument("file:///test.klg", 1, "2026-09-24\n    9:00 - ?\n\n2026-09-23\n    oops\n")
	if got := invalid.codeActions(nil, now); len(got) != 0 {
		t.Errorf("offered actions for an invalid file: %+v", got)
	}
}

func applyEdits(text string, d *document, edits []TextEdit) string {
	if len(edits) != 1 {
		panic("expected exactly one edit")
	}
	e := edits[0]
	return text[:d.offset(e.Range.Start)] + e.NewText + text[d.offset(e.Range.End):]
}

func titles(actions []CodeAction) []string {
	var result []string
	for _, a := range actions {
		result = append(result, a.Title)
	}
	return result
}
