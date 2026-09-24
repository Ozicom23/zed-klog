package main

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jotaen/klog/klog"
	"github.com/jotaen/klog/klog/parser"
	"github.com/jotaen/klog/klog/parser/txt"
)

// document is a parsed klog file.
//
// klog's parser rejects the whole file if any record is invalid. To keep
// totals, hovers and completions working while the user is typing, each
// record (block of lines) is parsed on its own instead.
type document struct {
	uri     string
	version int
	text    string
	lines   []string // Without line endings.
	records []*recordInfo
	errors  []parseError
	// headerLines are the first lines of all records, valid or not.
	headerLines map[int]bool
}

// recordInfo is a successfully parsed record, along with where it is located.
type recordInfo struct {
	record         klog.Record
	headerLine     int
	lastLine       int
	hasShouldTotal bool
	// entries holds the first and last line of each entry, in the same order
	// as record.Entries(). It is nil in the unlikely case that the lines
	// cannot be matched up with the entries.
	entries []lineSpan
}

type lineSpan struct{ first, last int }

// parseError is a klog parser error, with its line number in the file.
type parseError struct {
	line    int
	start   int // In runes.
	length  int // In runes.
	code    string
	title   string
	details string
}

func parseDocument(uri string, version int, text string) *document {
	d := &document{uri: uri, version: version, text: text, lines: splitLines(text), headerLines: map[int]bool{}}
	consumed, lineCount := 0, 0
	for consumed < len(text) {
		block, n := txt.ParseBlock(text[consumed:], lineCount)
		if n == 0 || block == nil {
			break
		}
		consumed += n
		lineCount += len(block.Lines())
		d.parseBlock(block)
	}
	return d
}

func (d *document) parseBlock(block txt.Block) {
	var source strings.Builder
	for _, l := range block.Lines() {
		source.WriteString(l.Original())
	}
	records, _, errs := parser.NewSerialParser().Parse(source.String())
	firstLine := block.OverallLineIndex(0)
	significant, head, _ := block.SignificantLines()
	d.headerLines[firstLine+head] = true
	if errs != nil {
		for _, e := range errs {
			d.errors = append(d.errors, parseError{
				line:    firstLine + e.LineNumber() - 1,
				start:   e.Position(),
				length:  e.Length(),
				code:    e.Code(),
				title:   e.Title(),
				details: e.Details(),
			})
		}
		return
	}
	if len(records) != 1 {
		return
	}

	info := &recordInfo{
		record:     records[0],
		headerLine: firstLine + head,
		lastLine:   firstLine + head + len(significant) - 1,
		// The parser has verified the headline, so any `(` is the should-total.
		hasShouldTotal: strings.Contains(significant[0].Text, "("),
	}

	// Find the entry lines, the same way klog's parser does: the first indented
	// line determines the indentation style, and lines that are indented twice
	// continue the summary of the previous entry.
	var indentator *txt.Indentator
	for i, l := range significant[1:] {
		lineIndex := info.headerLine + 1 + i
		if indentator == nil {
			indentator = txt.NewIndentator(txt.Indentations, l)
			if indentator == nil {
				continue // Record summary.
			}
		}
		if len(info.entries) > 0 && indentator.NewIndentedParseable(l, 2) != nil {
			info.entries[len(info.entries)-1].last = lineIndex
		} else {
			info.entries = append(info.entries, lineSpan{lineIndex, lineIndex})
		}
	}
	if len(info.entries) != len(info.record.Entries()) {
		info.entries = nil
	}
	d.records = append(d.records, info)
}

// recordAt returns the record that the given line belongs to.
func (d *document) recordAt(line int) *recordInfo {
	for _, r := range d.records {
		if line >= r.headerLine && line <= r.lastLine {
			return r
		}
	}
	return nil
}

func (d *document) klogRecords() []klog.Record {
	result := make([]klog.Record, len(d.records))
	for i, r := range d.records {
		result[i] = r.record
	}
	return result
}

func (d *document) line(i int) string {
	if i < 0 || i >= len(d.lines) {
		return ""
	}
	return d.lines[i]
}

// splitLines splits text the same way klog does: at `\n`, with an optional
// preceding `\r` belonging to the line ending.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// LSP positions count UTF-16 code units, while Go strings are UTF-8 and klog
// counts runes. These helpers convert between them.

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// runesToUTF16 returns the UTF-16 length of the first `runes` runes of `s`.
func runesToUTF16(s string, runes int) int {
	n := 0
	for _, r := range s {
		if runes <= 0 {
			break
		}
		n += utf16.RuneLen(r)
		runes--
	}
	return n
}

// utf16ToByte returns the byte offset in `s` of the given UTF-16 offset.
func utf16ToByte(s string, offset int) int {
	units := 0
	for i, r := range s {
		if units >= offset {
			return i
		}
		units += utf16.RuneLen(r)
	}
	return len(s)
}

// position converts a byte offset within the text to an LSP position.
func (d *document) position(offset int) Position {
	line := strings.Count(d.text[:offset], "\n")
	lineStart := strings.LastIndexByte(d.text[:offset], '\n') + 1
	return Position{Line: line, Character: utf16Len(d.text[lineStart:offset])}
}

// offset converts an LSP position to a byte offset within the text.
func (d *document) offset(p Position) int {
	lineStart := 0
	for i := 0; i < p.Line; i++ {
		next := strings.IndexByte(d.text[lineStart:], '\n')
		if next == -1 {
			return len(d.text)
		}
		lineStart += next + 1
	}
	return lineStart + utf16ToByte(d.line(p.Line), p.Character)
}

// minimalEdit returns a single edit that turns the document's text into
// `newText`, touching as little of the text as possible.
func (d *document) minimalEdit(newText string) TextEdit {
	old := d.text
	prefix := 0
	for prefix < len(old) && prefix < len(newText) && old[prefix] == newText[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(old) && !utf8.RuneStart(old[prefix]) {
		prefix--
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(newText)-prefix &&
		old[len(old)-1-suffix] == newText[len(newText)-1-suffix] {
		suffix++
	}
	for suffix > 0 && !utf8.RuneStart(old[len(old)-suffix]) {
		suffix--
	}
	return TextEdit{
		Range:   Range{Start: d.position(prefix), End: d.position(len(old) - suffix)},
		NewText: newText[prefix : len(newText)-suffix],
	}
}
