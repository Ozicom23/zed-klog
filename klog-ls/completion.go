package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jotaen/klog/klog"
)

var (
	datePrefixPattern  = regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}`)
	tagNamePrefix      = regexp.MustCompile(`#([\p{L}\d_-]*)$`)
	tagValuePrefix     = regexp.MustCompile(`#([\p{L}\d_-]+)=("[^"]*|'[^']*|[\p{L}\d_-]*)$`)
	unquotedTagPattern = regexp.MustCompile(`^[\p{L}\d_-]+$`)
)

// tagUsage is how often a tag name or tag value occurs in the file.
type tagUsage struct {
	text  string // As written the first time, e.g. `#Home-Office` or `"Liz Jones"`.
	count int
}

// completion completes tag names after `#`, and tag values after `#name=`.
func (d *document) completion(p Position) CompletionList {
	result := CompletionList{Items: []CompletionItem{}}
	line := d.line(p.Line)
	if datePrefixPattern.MatchString(line) {
		return result // Tags cannot appear in the date line.
	}
	prefix := line[:utf16ToByte(line, p.Character)]
	names, values := d.tagUsages(d.offset(p))

	if m := tagValuePrefix.FindStringSubmatchIndex(prefix); m != nil {
		name := strings.ToLower(prefix[m[2]:m[3]])
		edit := Range{Start: Position{p.Line, utf16Len(prefix[:m[4]])}, End: p}
		for _, u := range sortedUsages(values[name]) {
			result.Items = append(result.Items, completionItem(u, edit, CompletionItemKindValue))
		}
		return result
	}
	if m := tagNamePrefix.FindStringIndex(prefix); m != nil {
		edit := Range{Start: Position{p.Line, utf16Len(prefix[:m[0]])}, End: p}
		for _, u := range sortedUsages(names) {
			result.Items = append(result.Items, completionItem(u, edit, CompletionItemKindKeyword))
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
			name := line[m[2]:m[3]]
			key := strings.ToLower(name)
			if names[key] == nil {
				names[key] = &tagUsage{text: "#" + name}
			}
			names[key].count++

			if m[6] == -1 {
				continue
			}
			value := unquote(line[m[6]:m[7]])
			if value == "" {
				continue
			}
			if values[key] == nil {
				values[key] = map[string]*tagUsage{}
			}
			if values[key][value] == nil {
				values[key][value] = &tagUsage{text: quoteTagValue(value)}
			}
			values[key][value].count++
		}
		lineStart += len(line)
	}
	return names, values
}

func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// quoteTagValue quotes a tag value if necessary, like klog does.
func quoteTagValue(value string) string {
	switch {
	case unquotedTagPattern.MatchString(value):
		return value
	case strings.Contains(value, `"`):
		return "'" + value + "'"
	default:
		return `"` + value + `"`
	}
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
