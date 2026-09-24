; Summaries are free text. Like klog's official Sublime Text syntax, they are
; highlighted as comments. The more specific patterns below take precedence.
(record_summary) @comment

(entry_summary) @comment

(date) @title

(should_total
  [
    "("
    ")"
  ] @punctuation.bracket)

(should_total
  "!" @punctuation.special)

(duration) @number

(time) @constant

(time
  [
    "<"
    ">"
  ] @operator)

(range
  "-" @operator)

(open_range
  "-" @operator)

(placeholder) @constant.builtin

(tag) @tag

(tag
  "=" @operator)

(tag_value) @string
