package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jotaen/klog/klog"
	"github.com/jotaen/klog/klog/service"
)

// inlayHints shows each record's total at the end of its date line.
func (d *document) inlayHints(r Range, now time.Time) []InlayHint {
	result := []InlayHint{}
	for _, info := range d.records {
		if info.headerLine < r.Start.Line || info.headerLine > r.End.Line {
			continue
		}
		header := strings.TrimRight(d.line(info.headerLine), " \t")
		result = append(result, InlayHint{
			Position:    Position{info.headerLine, utf16Len(header)},
			Label:       totalOf(info.record, now).label(info),
			PaddingLeft: true,
		})
	}
	return result
}

var (
	durationValuePattern = regexp.MustCompile(`^\s+([+-]?(?:\d+h\d+m|\d+h|\d+m))`)
	rangeValuePattern    = regexp.MustCompile(
		`^\s+(<?\d{1,2}:\d{2}(?:am|pm)?>?\s*-\s*(?:<?\d{1,2}:\d{2}(?:am|pm)?>?|\?+))`)
)

// hover describes the tag, entry or record under the cursor.
func (d *document) hover(p Position, now time.Time) *Hover {
	line := d.line(p.Line)
	cursor := utf16ToByte(line, p.Character)

	for _, loc := range klog.HashTagPattern.FindAllStringIndex(line, -1) {
		if cursor >= loc[0] && cursor < loc[1] {
			return d.tagHover(line[loc[0]:loc[1]], d.lineRange(p.Line, loc[0], loc[1]))
		}
	}

	info := d.recordAt(p.Line)
	if info == nil {
		return nil
	}
	if p.Line == info.headerLine {
		return &Hover{Contents: markdown(recordDescription(info, now))}
	}
	for i, span := range info.entries {
		if span.first != p.Line {
			continue
		}
		entry := info.record.Entries()[i]
		loc := rangeValuePattern.FindStringSubmatchIndex(line)
		if loc == nil {
			loc = durationValuePattern.FindStringSubmatchIndex(line)
		}
		if loc == nil || cursor < loc[2] || cursor >= loc[3] {
			return nil
		}
		text := entryDescription(info.record, entry, now)
		if text == "" {
			return nil
		}
		return &Hover{Contents: markdown(text), Range: d.lineRange(p.Line, loc[2], loc[3])}
	}
	return nil
}

func recordDescription(info *recordInfo, now time.Time) string {
	r := info.record
	date := time.Date(r.Date().Year(), time.Month(r.Date().Month()), r.Date().Day(), 0, 0, 0, 0, time.UTC)
	t := totalOf(r, now)

	var b strings.Builder
	fmt.Fprintf(&b, "**%s**\n\n", date.Format("Monday, 2 January 2006"))
	fmt.Fprintf(&b, "- Total: **%s**", t.total.ToString())
	if t.running != nil {
		b.WriteString(" so far")
	}
	b.WriteString("\n")
	if info.hasShouldTotal {
		fmt.Fprintf(&b, "- Should-total: %s\n", r.ShouldTotal().ToString())
		fmt.Fprintf(&b, "- Diff: %s\n", service.Diff(r.ShouldTotal(), t.total).ToStringWithSign())
	}
	fmt.Fprintf(&b, "- Entries: %d\n", len(r.Entries()))
	if or := r.OpenRange(); or != nil {
		if t.running != nil {
			fmt.Fprintf(&b, "- Open range since %s, running for %s\n", or.Start().ToString(), t.running.ToString())
		} else {
			fmt.Fprintf(&b, "- Open range since %s, not counted\n", or.Start().ToString())
		}
	}
	return b.String()
}

func entryDescription(r klog.Record, e klog.Entry, now time.Time) string {
	return klog.Unbox[string](&e,
		func(rg klog.Range) string {
			return fmt.Sprintf("**%s** from %s to %s", rg.Duration().ToString(),
				describeTime(rg.Start()), describeTime(rg.End()))
		},
		func(klog.Duration) string { return "" },
		func(or klog.OpenRange) string {
			text := "Open range since " + describeTime(or.Start())
			if running, ok := runningTime(r.Date(), or, now); ok {
				return text + ", running for **" + running.ToString() + "**"
			}
			return text + ". It is not counted until it is closed."
		},
	)
}

func describeTime(t klog.Time) string {
	s := strings.Trim(t.ToString(), "<>")
	switch {
	case t.IsYesterday():
		return s + " (the day before)"
	case t.IsTomorrow():
		return s + " (the next day)"
	}
	return s
}

func (d *document) tagHover(text string, r *Range) *Hover {
	tag, err := klog.NewTagFromString(text)
	if err != nil {
		return nil
	}
	total, count := tagTotal(d.klogRecords(), tag)
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**: %s in %s", tag.ToString(), total.ToString(), plural(count, "entry", "entries"))
	if tag.Value() != "" {
		base := klog.NewTagOrPanic(tag.Name(), "")
		total, count := tagTotal(d.klogRecords(), base)
		fmt.Fprintf(&b, "\n\n**%s** (any value): %s in %s", base.ToString(), total.ToString(), plural(count, "entry", "entries"))
	}
	return &Hover{Contents: markdown(b.String()), Range: r}
}

// tagTotal sums up the entries with the given tag, like `klog total --tag`
// does: a tag in the record summary applies to all entries of the record.
func tagTotal(records []klog.Record, tag klog.Tag) (klog.Duration, int) {
	total, count := klog.NewDuration(0, 0), 0
	for _, r := range records {
		for _, e := range r.Entries() {
			tags := klog.Merge(r.Summary().Tags(), e.Summary().Tags())
			if tags.Contains(tag) {
				total = total.Plus(e.Duration())
				count++
			}
		}
	}
	return total, count
}

func plural(n int, singular string, pluralForm string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, pluralForm)
}

func markdown(text string) MarkupContent {
	return MarkupContent{Kind: "markdown", Value: text}
}

// lineRange converts a byte range within a line to an LSP range.
func (d *document) lineRange(line int, start int, end int) *Range {
	text := d.line(line)
	return &Range{
		Start: Position{line, utf16Len(text[:start])},
		End:   Position{line, utf16Len(text[:end])},
	}
}
