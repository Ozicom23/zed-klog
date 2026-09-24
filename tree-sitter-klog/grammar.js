/**
 * @file Tree-sitter grammar for klog, the plain-text time tracking format.
 * @license MIT
 * @see https://github.com/jotaen/klog/blob/main/Specification.md
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

// Tag names and unquoted tag values may only contain letters, digits, `_` and `-`.
const TAG_CHARS = /[\p{L}0-9_-]+/;

// Wall clock time, e.g. `8:30`, `14:00` or `9:30am`.
const CLOCK = /\d{1,2}:\d{2}(am|pm)?/;

module.exports = grammar({
  name: 'klog',

  // Line structure is significant in klog, so line breaks are produced by the
  // external scanner (src/scanner.c). It keeps track of the indentation style of
  // the current record, which determines whether an indented line starts a new
  // entry or continues the summary of the previous entry.
  externals: $ => [
    // Line break, followed by a line that is not indented (record summary).
    $._line_break,
    // Line break, followed by a line that is indented once (entry).
    $._entry_break,
    // Line break, followed by a line that is indented twice (entry summary).
    $._continuation_break,
    // Line break(s) ending a record: blank lines or the end of the file.
    $._record_separator,
    // Never valid in the grammar; tells the scanner that the parser is
    // performing error recovery.
    $._error_sentinel,
  ],

  extras: _ => [/[ \t]/],

  rules: {
    source_file: $ => repeat(choice($.record, $._record_separator)),

    record: $ => seq(
      field('date', $.date),
      optional(field('should_total', $.should_total)),
      optional(seq($._line_break, field('summary', $.record_summary))),
      repeat(seq($._entry_break, field('entry', $.entry))),
    ),

    date: _ => token(choice(
      /\d{4}-\d{2}-\d{2}/,
      /\d{4}\/\d{2}\/\d{2}/,
    )),

    should_total: $ => seq(
      '(',
      field('duration', $.duration),
      token.immediate('!'),
      ')',
    ),

    record_summary: $ => seq(
      $._summary_line,
      repeat(seq($._line_break, $._summary_line)),
    ),

    entry: $ => seq(
      field('value', choice($.duration, $.range, $.open_range)),
      optional(field('summary', $.entry_summary)),
    ),

    // The summary either starts on the entry line, or on the line below it.
    entry_summary: $ => choice(
      seq(
        $._summary_line,
        repeat(seq($._continuation_break, $._summary_line)),
      ),
      repeat1(seq($._continuation_break, $._summary_line)),
    ),

    duration: _ => token(seq(
      optional(/[+-]/),
      choice(/\d+h\d+m/, /\d+h/, /\d+m/),
    )),

    range: $ => seq(
      field('start', $.time),
      '-',
      field('end', $.time),
    ),

    open_range: $ => seq(
      field('start', $.time),
      '-',
      field('end', $.placeholder),
    ),

    // A time can be shifted to the previous day (`<23:00`) or to the next
    // day (`1:30>`), but not both.
    time: _ => choice(
      seq('<', token.immediate(CLOCK)),
      seq(CLOCK, optional(token.immediate('>'))),
    ),

    placeholder: _ => /\?+/,

    _summary_line: $ => repeat1(choice(
      $._text,
      $.tag,
      // A `#` that is not followed by a tag name is regular text.
      '#',
    )),

    // Free text. It stops at `#`, so that tags can appear anywhere in a line.
    _text: _ => token(prec(-1, /[^# \t\r\n][^#\r\n]*/)),

    tag: $ => seq(
      '#',
      field('name', $.tag_name),
      optional(seq(
        token.immediate(prec(1, '=')),
        optional(field('value', $.tag_value)),
      )),
    ),

    tag_name: _ => token.immediate(prec(1, TAG_CHARS)),

    // A quoted value without a closing quote on the same line is not a value;
    // in that case the quote and everything after it is regular text.
    tag_value: _ => token.immediate(prec(1, choice(
      /"[^"\r\n]*"/,
      /'[^'\r\n]*'/,
      TAG_CHARS,
    ))),
  },
});
