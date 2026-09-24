/**
 * External scanner for klog.
 *
 * klog is line based, and the meaning of an indented line depends on the
 * indentation style of its record: 2, 3 or 4 spaces, or a tab. The style is
 * set by the first indented line of the record, which is always an entry.
 * After that, a line indented once starts a new entry, while a line indented
 * twice continues the summary of the previous entry.
 *
 * This scanner produces all line breaks, classified by what the next line
 * looks like. The line break and the indentation of the next line are skipped
 * as padding, so every token is zero-width and sits at the first character of
 * the next line's content. That way, no node includes line breaks or
 * indentation.
 */

#include "tree_sitter/alloc.h"
#include "tree_sitter/parser.h"

#include <stdbool.h>
#include <stdint.h>

enum TokenType {
  LINE_BREAK,
  ENTRY_BREAK,
  CONTINUATION_BREAK,
  RECORD_SEPARATOR,
  ERROR_SENTINEL,
};

/** Indentation style of the current record; `indent_char` is 0 if not known yet. */
typedef struct {
  uint8_t indent_char;
  uint8_t indent_width;
} Scanner;

static inline bool is_blank(int32_t c) { return c == ' ' || c == '\t'; }

static inline bool is_newline(int32_t c) { return c == '\n' || c == '\r'; }

static inline bool is_digit(int32_t c) { return c >= '0' && c <= '9'; }

/** Checks whether the upcoming text starts like a date, e.g. `2020-01-01`. */
static bool looks_like_date(TSLexer *lexer) {
  for (int i = 0; i < 4; i++) {
    if (!is_digit(lexer->lookahead)) {
      return false;
    }
    lexer->advance(lexer, false);
  }
  return lexer->lookahead == '-' || lexer->lookahead == '/';
}

void *tree_sitter_klog_external_scanner_create(void) {
  return ts_calloc(1, sizeof(Scanner));
}

void tree_sitter_klog_external_scanner_destroy(void *payload) {
  ts_free(payload);
}

unsigned tree_sitter_klog_external_scanner_serialize(void *payload, char *buffer) {
  Scanner *scanner = payload;
  buffer[0] = (char)scanner->indent_char;
  buffer[1] = (char)scanner->indent_width;
  return 2;
}

void tree_sitter_klog_external_scanner_deserialize(void *payload, const char *buffer, unsigned length) {
  Scanner *scanner = payload;
  scanner->indent_char = 0;
  scanner->indent_width = 0;
  if (length == 2) {
    scanner->indent_char = (uint8_t)buffer[0];
    scanner->indent_width = (uint8_t)buffer[1];
  }
}

bool tree_sitter_klog_external_scanner_scan(void *payload, TSLexer *lexer, const bool *valid_symbols) {
  Scanner *scanner = payload;
  bool error_recovery = valid_symbols[ERROR_SENTINEL];

  // Trailing blanks of the current line.
  while (is_blank(lexer->lookahead)) {
    lexer->advance(lexer, true);
  }
  if (!is_newline(lexer->lookahead)) {
    return false;
  }

  // Skip the line break and the indentation of the next line. If that line is
  // blank, keep going until the next non-blank line or the end of the file.
  bool blank_line_seen = false;
  int32_t indent_char = 0;
  uint32_t indent_run = 0; // How often `indent_char` repeats at the line start.
  for (;;) {
    if (lexer->lookahead == '\r') {
      lexer->advance(lexer, true);
    }
    if (lexer->lookahead == '\n') {
      lexer->advance(lexer, true);
    }

    indent_char = 0;
    indent_run = 0;
    bool run_ended = false;
    while (is_blank(lexer->lookahead)) {
      if (indent_char == 0) {
        indent_char = lexer->lookahead;
      }
      if (!run_ended && lexer->lookahead == indent_char) {
        indent_run++;
      } else {
        run_ended = true;
      }
      lexer->advance(lexer, true);
    }

    if (is_newline(lexer->lookahead)) {
      blank_line_seen = true;
      continue;
    }
    if (lexer->eof(lexer)) {
      blank_line_seen = true;
    }
    break;
  }
  lexer->mark_end(lexer);

  enum TokenType token;
  if (blank_line_seen) {
    token = RECORD_SEPARATOR;
  } else if (indent_char == 0) {
    token = LINE_BREAK;
  } else if (
    scanner->indent_char != 0 &&
    indent_char == scanner->indent_char &&
    indent_run >= 2u * scanner->indent_width
  ) {
    token = CONTINUATION_BREAK;
  } else {
    token = ENTRY_BREAK;
  }

  if (error_recovery) {
    // Every token is valid during error recovery, so use a heuristic to pick
    // the more likely meaning of a non-indented line.
    if (token == LINE_BREAK && looks_like_date(lexer)) {
      token = RECORD_SEPARATOR;
    }
  } else {
    if (token == CONTINUATION_BREAK && !valid_symbols[CONTINUATION_BREAK]) {
      token = ENTRY_BREAK;
    }
    // A line that cannot continue the current record ends it. This happens,
    // for example, when two records are not separated by a blank line.
    if (!valid_symbols[token]) {
      token = RECORD_SEPARATOR;
    }
    if (!valid_symbols[token]) {
      return false;
    }
  }

  if (token == LINE_BREAK || token == RECORD_SEPARATOR) {
    // Non-indented lines only appear before the first entry of a record.
    scanner->indent_char = 0;
    scanner->indent_width = 0;
  } else if (scanner->indent_char == 0) {
    // First indented line of the record: it determines the indentation style.
    // Like klog, recognise up to 4 spaces as one level.
    scanner->indent_char = (uint8_t)indent_char;
    scanner->indent_width = indent_char == '\t' ? 1 : (uint8_t)(indent_run < 4 ? indent_run : 4);
  }

  lexer->result_symbol = token;
  return true;
}
