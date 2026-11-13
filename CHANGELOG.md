# Changelog

## v1.0.0

dockerfmt 1.0 makes formatting safe by construction and fixes a long list of
bugs found by fuzzing and review. Some defaults changed; see below.

### Breaking changes

- **The trailing newline is kept as it is.** Without `--newline` or an
  EditorConfig `insert_final_newline`, the output now ends with a newline if
  the input does. Before, it never did unless `--newline` was given.
  `--newline=false` removes it. The JS `trailingNewline` option defaults the
  same way.
- **The npm package requires Node.js 20 or later.**
- **The Go `lib` package exposes only `Format`, `Config` and the indent size
  constants.** `FormatFileLines`, `ExtendedNode`, `ParseState`, `FormatNode`,
  `FormatOnBuild`, `BuildExtendedNode`, `GetHeredoc`, `GetFileLines`,
  `StripWhitespace`, `FormatComments` and `IndentFollowingLines` were removed.
  `Format` returns an error instead of exiting the process.
- **The default Docker image is based on distroless** and has no shell. Use
  `ghcr.io/reteps/dockerfmt:alpine` where a shell is needed, such as GitLab CI.
- `--indent` must be from 1 to 16, and `--write` can't be combined with
  `--check` or `--diff`.
- Help is shown only when no files are given and stdin is a terminal; empty
  piped input is an error.

### Safety

- Each formatted instruction is checked with BuildKit's parser to mean the
  same as the original, and to stay the same when formatted again. The whole
  file is then checked as written. Anything that fails is left unchanged, with
  a warning.
- `FuzzFormat` checks these properties on random inputs; CI runs it on every
  change.
- `--write` rewrites files in place: permissions, owner, hard links and
  symbolic links are kept, it works in read-only directories, and a failed
  write restores the original contents.

### Build checks

Formatting now fixes these BuildKit build-check warnings, none of which change
the build:

- FromAsCasing: `from a as b` becomes `FROM a AS b`.
- ExposeProtoCasing: `EXPOSE 80/TCP` becomes `EXPOSE 80/tcp`.
- LegacyKeyValueFormat: `LABEL key value` is rewritten as `LABEL key=value`,
  as `ENV` already was, and so are values with quotes or escapes, whose
  whitespace is escaped. A value written across several lines keeps its line
  breaks inside quotes.
- NoEmptyContinuation: blank lines inside `ENV`, `LABEL` and other
  continued instructions are removed (BuildKit ignores them, and will reject
  them in a future release). Lines holding only `\` are kept.

### Formatting

- Indentation with tabs: `--use-tabs`, `indent_style = tab` in EditorConfig,
  or `useTabs` (or `indent: 0`, as prettier-plugin-sh passes) in JS.

- `HEALTHCHECK` is formatted: flags and `CMD`/`NONE` are normalized, the
  command is formatted like `CMD`'s, and `CMD` stays on its own line when
  written that way.
- Words in `ENV`, `LABEL`, `ARG`, `VOLUME`, `USER` and `STOPSIGNAL` are
  separated by single spaces, trailing whitespace is removed, and a space goes
  before a line continuation that ends a word. `VOLUME`'s JSON form is
  normalized.

### Fixes

- Quoted flag values (`--chown="my user"`) kept their quotes; `--` ending the
  flags is kept.
- Backquoted command substitutions are no longer rewritten as `$(...)`, which
  could change what they run.
- Stages built on a PowerShell stage or a Windows image are not formatted as
  POSIX shell.
- Blank lines after a comment are collapsed to one instead of deleted.
- `# dockerfmt-ignore` applies only as the last comment before an
  instruction.
- Indented comments that would become `# syntax=` or `# check=` directives
  keep their indentation.
- Flags and continuations use the file's escape character with `# escape=`.
- `<<-` heredoc scripts are indented with tabs.
- Many edge cases with comments, separators, stray carriage returns and
  continuations at the end of the file.

### Other changes

- `-` reads stdin; `version` reports the module version for `go install`
  builds.
- EditorConfig property names are case-insensitive, `indent_size = tab` uses
  `tab_width`, and invalid settings are reported once as warnings.
- The JS API validates its options, reuses one WASM instance, and has an
  `onFallback` option.
- The npm launcher passes signals through and explains launch failures.
- Release binaries include darwin/amd64; prereleases aren't tagged `latest`.
- The pre-commit hook matches Dockerfile names case-insensitively.
