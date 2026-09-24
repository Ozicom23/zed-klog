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

	// The missing `)` is reported at the end of the line, so the whole line is marked.
	e := diagnostics[0]
	wantErrorRange := Range{Position{3, 0}, Position{3, 14}}
	if e.Severity != SeverityError || e.Code != "ErrorMalformedPropertiesSyntax" || e.Range != wantErrorRange {
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

func TestWarningsAreAttachedToTheirRecord(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, `2026-09-01
    9:00 - ?

2026-09-01
    1h

2026-09-23
    22:00 - ?

2026-09-24
    8:00 - 9:00
`)
	var lines []int
	for _, w := range d.diagnostics(now) {
		if w.Severity != SeverityWarning || w.Message != "Unclosed open range" {
			t.Errorf("unexpected diagnostic: %+v", w)
		}
		lines = append(lines, w.Range.Start.Line)
	}
	// Only the first of the two records on 2026-09-01 has an open range. The
	// one from yesterday is unclosed, because there is a record for today.
	if len(lines) != 2 || lines[0] != 0 || lines[1] != 6 {
		t.Errorf("got warnings at lines %v, want [0 6]", lines)
	}

	// Without a record for today, yesterday's open range may still be running.
	d = parseDocument("file:///test.klg", 1, "2026-09-23\n    22:00 - ?\n")
	if got := d.diagnostics(now); len(got) != 0 {
		t.Errorf("unexpected diagnostics: %+v", got)
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

func TestCompletionReplacesTheWholeTag(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-24\n    1h #ticket=A-1 #call=\"Liz Jones\"\n    2h #tixet #call=\"Lix Jones\" done\n")
	tests := []struct {
		name string
		pos  Position
		want TextEdit
	}{
		{"tag name", Position{2, 10}, TextEdit{Range{Position{2, 7}, Position{2, 13}}, "#ticket"}},
		{"quoted value", Position{2, 22}, TextEdit{Range{Position{2, 20}, Position{2, 31}}, `"Liz Jones"`}},
	}
	for _, tt := range tests {
		var edit *TextEdit
		for _, item := range d.completion(tt.pos).Items {
			if item.Label == tt.want.NewText {
				edit = item.TextEdit
			}
		}
		if edit == nil || *edit != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, edit, tt.want)
		}
	}
}

func TestCompletionAfterUnclosedQuoteCompletesTagNames(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-24\n    1h #a=\"x\" #b\n    2h #a=\"foo #")
	items := d.completion(Position{2, 16}).Items
	if len(items) == 0 || items[0].Kind != CompletionItemKindKeyword {
		t.Errorf("expected tag names, got %+v", items)
	}
}

func TestCompletionInSummaryLineThatStartsWithADate(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-24\n2026-09-20 was planned #plan #\n    1h\n")
	if got := d.completion(Position{1, 30}).Items; len(got) != 1 || got[0].Label != "#plan" {
		t.Errorf("got %+v", got)
	}
	if got := d.completion(Position{0, 10}).Items; len(got) != 0 {
		t.Errorf("offered completions in the date line: %+v", got)
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
		line  int // Where the cursor is.
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
			name:  "stop with the 12-hour clock",
			text:  "2026-09-24\n    9:00am - ?\n",
			title: "Stop open range at 2:32pm",
			want:  "2026-09-24\n    9:00am - 2:32pm\n",
		},
		{
			name:  "start in today's record",
			text:  "2026-09-24\n  8:00 - 9:00\n",
			title: "Start open range at 14:32",
			want:  "2026-09-24\n  8:00 - 9:00\n  14:32 - ?\n",
		},
		{
			name:  "start with the 12-hour clock",
			text:  "2026-09-24\n  8:00am - 9:00am\n",
			title: "Start open range at 2:32pm",
			want:  "2026-09-24\n  8:00am - 9:00am\n  2:32pm - ?\n",
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
			line:  2,
			title: "Add record for today: 2026-09-24 (7h30m!)",
			want:  "2026-09-20 (7h30m!)\n    2h\n\n2026-09-22\n    1h\n\n2026-09-24 (7h30m!)\n",
		},
		{
			name:  "create with slashes",
			text:  "2026/09/22\n    1h\n",
			title: "Add record for today: 2026/09/24",
			want:  "2026/09/22\n    1h\n\n2026/09/24\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parseDocument("file:///test.klg", 1, tt.text)
			actions := d.codeActions(tt.line, nil, now)
			for _, a := range actions {
				if a.Title != tt.title {
					continue
				}
				if got := applyEdits(d.text, d, a.Edit.Changes[d.uri]); got != tt.want {
					t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
				}
				return
			}
			t.Errorf("no action %q in %+v", tt.title, titles(actions))
		})
	}
}

func TestCodeActionsAreOnlyOfferedWhenAppropriate(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-24\n    9:00 - ?\n")
	if got := titles(d.codeActions(1, nil, now)); strings.Join(got, "|") != "Stop open range at 14:32" {
		t.Errorf("got %q; starting another open range or creating today's record should not be offered", got)
	}
	if got := d.codeActions(1, []string{"source.fixAll"}, now); len(got) != 0 {
		t.Errorf("offered actions for other kinds: %+v", got)
	}
	if got := d.codeActions(1, []string{"refactor"}, now); len(got) != 1 {
		t.Errorf("did not offer actions for the parent kind: %+v", got)
	}

	invalid := parseDocument("file:///test.klg", 1, "2026-09-24\n    9:00 - ?\n\n2026-09-23\n    oops\n")
	if got := invalid.codeActions(0, nil, now); len(got) != 0 {
		t.Errorf("offered actions for an invalid file: %+v", got)
	}
}

func TestCodeActionsAreOnlyOfferedNearWhatTheyChange(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2026-09-01\n    1h\n\n2026-09-24\n    9:00 - ?\n\n")
	tests := []struct {
		line int
		want string
	}{
		{0, ""},                         // An old record.
		{1, ""},                         // An entry of the old record.
		{2, "Stop open range at 14:32"}, // Between records.
		{4, "Stop open range at 14:32"}, // In today's record.
		{6, "Stop open range at 14:32"}, // After the last record.
	}
	for _, tt := range tests {
		if got := strings.Join(titles(d.codeActions(tt.line, nil, now)), "|"); got != tt.want {
			t.Errorf("line %d: got %q, want %q", tt.line, got, tt.want)
		}
	}

	// Without a record for today, starting and creating it is offered in the
	// latest record, but not in older ones.
	d = parseDocument("file:///test.klg", 1, "2026-09-01\n    1h\n\n2026-09-22\n    2h\n")
	if got := titles(d.codeActions(0, nil, now)); len(got) != 0 {
		t.Errorf("offered actions in an old record: %q", got)
	}
	want := "Start open range at 14:32 in a new record for today|Add record for today: 2026-09-24"
	if got := strings.Join(titles(d.codeActions(4, nil, now)), "|"); got != want {
		t.Errorf("latest record: got %q, want %q", got, want)
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
