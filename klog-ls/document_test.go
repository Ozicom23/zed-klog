package main

import (
	"testing"
)

func TestParseMapsRecordsAndEntriesToLines(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, `
2020-01-01 (8h!)
Record summary
    1h First
        continued
    8:00 - 9:00

2020-01-02
	<23:00 - 1:00
		starts the day before
	2h
`)
	if len(d.errors) != 0 {
		t.Fatalf("unexpected errors: %+v", d.errors)
	}
	if len(d.records) != 2 {
		t.Fatalf("got %d records, want 2", len(d.records))
	}

	first := d.records[0]
	if first.headerLine != 1 || first.lastLine != 5 || !first.hasShouldTotal {
		t.Errorf("first record: got header %d, last %d, should-total %v", first.headerLine, first.lastLine, first.hasShouldTotal)
	}
	assertSpans(t, first.entries, []lineSpan{{3, 4}, {5, 5}})

	second := d.records[1]
	if second.headerLine != 7 || second.lastLine != 10 || second.hasShouldTotal {
		t.Errorf("second record: got header %d, last %d, should-total %v", second.headerLine, second.lastLine, second.hasShouldTotal)
	}
	assertSpans(t, second.entries, []lineSpan{{8, 9}, {10, 10}})

	if d.recordAt(4) != first || d.recordAt(9) != second || d.recordAt(6) != nil {
		t.Error("recordAt returned the wrong record")
	}
}

func TestParseKeepsValidRecordsWhenOthersAreInvalid(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2020-01-01\n    1h\n\n2020-13-45\n    2h\n\n2020-01-03\n    xyz\n")
	if len(d.records) != 1 || d.records[0].headerLine != 0 {
		t.Fatalf("got %d valid records, want only the first one", len(d.records))
	}
	if len(d.errors) != 2 {
		t.Fatalf("got %d errors, want 2: %+v", len(d.errors), d.errors)
	}
	if e := d.errors[0]; e.line != 3 || e.code != "ErrorInvalidDate" {
		t.Errorf("first error: got line %d, code %s", e.line, e.code)
	}
	if e := d.errors[1]; e.line != 7 || e.code != "ErrorMalformedEntry" || e.start != 4 {
		t.Errorf("second error: got line %d, code %s, start %d", e.line, e.code, e.start)
	}
}

func TestPositionsUseUTF16(t *testing.T) {
	line := "a😀b読c"
	if got := utf16Len(line); got != 6 {
		t.Errorf("utf16Len: got %d, want 6", got)
	}
	if got := runesToUTF16(line, 3); got != 4 {
		t.Errorf("runesToUTF16: got %d, want 4", got)
	}
	if got := utf16ToByte(line, 3); got != 5 {
		t.Errorf("utf16ToByte: got %d, want 5", got)
	}

	d := parseDocument("file:///test.klg", 1, "x\r\n"+line+"\n")
	offset := d.offset(Position{1, 4})
	if got := d.text[offset:]; got != "読c\n" {
		t.Errorf("offset: points at %q", got)
	}
	if got := d.position(offset); got != (Position{1, 4}) {
		t.Errorf("position: got %+v", got)
	}
}

func TestMinimalEditOnlyTouchesChangedText(t *testing.T) {
	d := parseDocument("file:///test.klg", 1, "2020-01-01\n    8:00 - ?\n")
	edit := d.minimalEdit("2020-01-01\n    8:00 - 9:30\n")
	want := TextEdit{Range: Range{Position{1, 11}, Position{1, 12}}, NewText: "9:30"}
	if edit != want {
		t.Errorf("got %+v, want %+v", edit, want)
	}

	d = parseDocument("file:///test.klg", 1, "#ä")
	edit = d.minimalEdit("#ö")
	want = TextEdit{Range: Range{Position{0, 1}, Position{0, 2}}, NewText: "ö"}
	if edit != want {
		t.Errorf("multi-byte characters: got %+v, want %+v", edit, want)
	}
}

func assertSpans(t *testing.T, got []lineSpan, want []lineSpan) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got entries %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
