// Package lib formats Dockerfiles.
//
// A file is parsed with BuildKit, the parser docker build uses, and formatted
// one instruction at a time. Formatters work from each instruction's source
// text, because BuildKit's parse tree drops what a formatter has to keep:
// comments, blank lines and "\"-only lines inside continuations, and the
// original quoting and layout. Decisions about what an instruction means (JSON
// or shell form, heredocs, flags, the active SHELL) come from the parse tree.
//
// Shell-form RUN, CMD and ENTRYPOINT bodies are formatted with mvdan.cc/sh.
// Comment and separator lines, which aren't shell, are set aside and put back
// by syntax tree position (see formatShellBody).
//
// Every formatted instruction is parsed again and compared with BuildKit's
// reading of the original (see equivalent). If they differ, or formatting
// fails, the instruction is left as written, so a formatter bug can't change
// what a Dockerfile does.
package lib
