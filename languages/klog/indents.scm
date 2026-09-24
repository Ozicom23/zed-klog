; Zed requires an @indent capture. A date never spans multiple lines, so this
; one does not change the indentation.
(date) @indent

; Where records start. `decrease_indent_patterns` in config.toml aligns a date
; typed on an indented line with it.
(record) @start.record
