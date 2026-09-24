package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jotaen/klog/klog"
)

var (
	tagNamePrefix  = regexp.MustCompile(`#([\p{L}\d_-]*)$`)
	tagValuePrefix = regexp.MustCompile(`#([\p{L}\d_-]+)=("[^"]*|'[^']*|[\p{L}\d_-]*)$`)
	// The rest of a tag name or value after the cursor, which completions replace.
	tagCharsSuffix     = regexp.MustCompile(`^[\p{L}\d_-]*`)
	doubleQuotedSuffix = regexp.MustCompile(`^[^"]*"?`)
	singleQuotedSuffix = regexp.MustCompile(`^[^']*'?`)
)

// tagUsage is how often a tag name or tag value occurs in the file.
type tagUsage struct {
	text  string // As written the first time, e.g. `#Home-Office` or `"Liz Jones"`.
	count int
}

// completion completes tag names after `#`, and tag values after `#name=`.
func (d *document) completion(p Position) CompletionList {
	result := CompletionList{Items: []CompletionItem{}}
	if d.headerLines[p.Line] {
		return result // Tags cannot appear in the date line.
	}
	line := d.line(p.Line)
	cursor := utf16ToByte(line, p.Character)
	prefix, suffix := line[:cursor], line[cursor:]
	names, values := d.tagUsages(d.offset(p))

	// A tag name right before the cursor comes first: after a quote that is
	// not closed, klog starts a new tag rather than continuing the value.
	if m := tagNamePrefix.FindStringIndex(prefix); m != nil {
		end := cursor + len(tagCharsSuffix.FindString(suffix))
		for _, u := range sortedUsages(names) {
			result.Items = append(result.Items, completionItem(u, *d.lineRange(p.Line, m[0], end), CompletionItemKindKeyword))
		}
		return result
	}
	if m := tagValuePrefix.FindStringSubmatchIndex(prefix); m != nil {
		name := strings.ToLower(prefix[m[2]:m[3]])
		rest := tagCharsSuffix
		switch value := prefix[m[4]:m[5]]; {
		case strings.HasPrefix(value, `"`):
			rest = doubleQuotedSuffix
		case strings.HasPrefix(value, "'"):
			rest = singleQuotedSuffix
		}
		end := cursor + len(rest.FindString(suffix))
		for _, u := range sortedUsages(values[name]) {
			result.Items = append(result.Items, completionItem(u, *d.lineRange(p.Line, m[4], end), CompletionItemKindValue))
		}
	}
	return result
}

func completionItem(u tagUsage, edit Range, kind int) CompletionItem {
	return CompletionItem{
		Label:      u.text,
		Kind:       kind,
		Detail:     plural(u.count, "use", "uses"),
		SortText:   fmt.Sprintf("%08d", 1<<24-u.count),
		FilterText: u.text,
		TextEdit:   &TextEdit{Range: edit, NewText: u.text},
	}
}

// tagUsages collects all tags in the file, except for the one that is being
// typed at `cursor`. Like klog, it treats tag names as case-insensitive.
// It scans the text instead of the parsed records, to also include tags from
// records that are currently invalid.
func (d *document) tagUsages(cursor int) (names map[string]*tagUsage, values map[string]map[string]*tagUsage) {
	names = map[string]*tagUsage{}
	values = map[string]map[string]*tagUsage{}
	lineStart := 0
	for _, line := range strings.SplitAfter(d.text, "\n") {
		for _, m := range klog.HashTagPattern.FindAllStringSubmatchIndex(line, -1) {
			if cursor >= lineStart+m[0] && cursor <= lineStart+m[1] {
				continue
			}
			tag, err := klog.NewTagFromString(line[m[0]:m[1]])
			if err != nil {
				continue
			}
			name := tag.Name()
			if names[name] == nil {
				names[name] = &tagUsage{text: "#" + line[m[2]:m[3]]}
			}
			names[name].count++

			if tag.Value() == "" {
				continue
			}
			if values[name] == nil {
				values[name] = map[string]*tagUsage{}
			}
			if values[name][tag.Value()] == nil {
				// klog quotes the value if needed.
				values[name][tag.Value()] = &tagUsage{text: strings.TrimPrefix(tag.ToString(), "#"+name+"=")}
			}
			values[name][tag.Value()].count++
		}
		lineStart += len(line)
	}
	return names, values
}

func sortedUsages(usages map[string]*tagUsage) []tagUsage {
	result := make([]tagUsage, 0, len(usages))
	for _, u := range usages {
		result = append(result, *u)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].count != result[j].count {
			return result[i].count > result[j].count
		}
		return result[i].text < result[j].text
	})
	return result
}
