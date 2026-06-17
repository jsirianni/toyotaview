# AGENTS.md

## Purpose

This repository follows a compact Go style guide derived from the Uber Go Style Guide.
Use these rules when writing, modifying, reviewing, or generating Go code.

Primary goals:

- Produce idiomatic, maintainable Go.
- Prefer explicit ownership, lifecycle, and error handling.
- Keep APIs stable and unsurprising.
- Keep code easy for humans to review.
- Let `gofmt`, `goimports`, `go vet`, tests, and linters enforce the mechanical parts.

## Required Checks

On every code change, run formatting before verification:

```sh
make fmt
```

After formatting, start four concurrent sub agents using `gpt-5.4-mini` with low
reasoning. Give each sub agent only the repository path and one assigned command:

```sh
make test
make vet
make lint
make gosec
```

Do not pass full conversation history, implementation context, plans, diffs, or
unrelated instructions to the sub agents. Each sub agent must only run its
assigned make target and report whether it passed or failed.

Wait for all sub agents to finish. If all checks pass, no failure detail is
needed. If any check fails, the parent agent must run the failed target itself,
inspect the output, and decide how to proceed. Fix failures that are in scope
for the current code change. Prompt the user when the failure is out of scope,
unrelated, environmental, or requires a product decision.

Recommended baseline linters:

- `errcheck`
- `goimports`
- `govet`
- `staticcheck`
- `revive`

## General Go Rules

- Do not use pointers to interfaces. Pass interfaces as values.
- Verify interface compliance at compile time when it is part of an API contract:

  ```go
  var _ http.Handler = (*Handler)(nil)
  ```

- Choose pointer receivers when methods mutate the receiver or copying is expensive.
- Choose value receivers for immutable, small value types when appropriate.
- Be consistent with receiver type across methods on the same type.
- Do not shadow built-in names such as `error`, `string`, `len`, `cap`, `new`, `make`, or `copy`.
- Avoid mutable package globals. Prefer dependency injection.
- Prefix unexported package-level variables/constants with `_`, except unexported errors, which may use `err...`.
- Avoid `init()`. If unavoidable, keep it deterministic, side-effect-light, and free of I/O, environment reads, goroutines, and ordering dependencies.
- Do not start goroutines in `init()`.

## Errors

- Handle every error. Do not ignore errors unless there is a documented and intentional reason.
- Handle an error once:
  - Return it, optionally wrapped.
  - Log it and degrade gracefully.
  - Match it and handle it.
  - Do not both log and return the same error unless there is a specific boundary reason.
- Prefer returning errors over panics.
- Do not use panic/recover as normal control flow.
- In production code, panic only for truly unrecoverable programmer errors or safe startup-time initialization failures.
- In tests, prefer `t.Fatal`, `t.Fatalf`, `require.NoError`, or equivalent over panic.
- Use `errors.New` for static error strings.
- Use `fmt.Errorf` for dynamic error strings.
- If callers must match an error:
  - Use exported `var ErrSomething = errors.New(...)` for static errors.
  - Use custom error types ending in `Error` for dynamic, matchable errors.
- Name exported error values `ErrSomething`.
- Name unexported error values `errSomething`.
- Name custom error types `SomethingError`.
- Wrap errors with context using `fmt.Errorf("operation: %w", err)` when callers may need `errors.Is` or `errors.As`.
- Use `%v` instead of `%w` only when intentionally hiding the underlying error.
- Keep error context short. Prefer `new store: %w` over `failed to create new store: %w`.
- Use the comma-ok form for type assertions:

  ```go
  v, ok := x.(T)
  if !ok {
      return fmt.Errorf("expected T")
  }
  ```

## Program Exit

- Only `main` may call `os.Exit` or `log.Fatal`.
- Non-main code must return errors.
- Prefer a single exit point:

  ```go
  func main() {
      if err := run(); err != nil {
          fmt.Fprintln(os.Stderr, err)
          os.Exit(1)
      }
  }
  ```

- Keep business logic in `run` or lower-level functions so it is testable.

## Concurrency

- The zero value of `sync.Mutex` and `sync.RWMutex` is valid. Do not allocate mutexes with `new`.
- Do not embed mutexes. Use named fields:

  ```go
  type cache struct {
      mu sync.Mutex
      m  map[string]string
  }
  ```

- Use `defer` to unlock unless the hot path has proven nanosecond-level sensitivity.
- Avoid fire-and-forget goroutines.
- Every goroutine must have:
  - a predictable stop condition,
  - a way to signal shutdown,
  - and a way to wait until it exits.
- Use `context.Context`, `sync.WaitGroup`, stop/done channels, or equivalent lifecycle controls.
- Stop tickers and timers when done.
- Use goroutine leak tests where the package manages goroutines.
- Prefer typed atomic wrappers such as `go.uber.org/atomic` when that dependency is acceptable.

## Slices, Maps, and Ownership

- Copy slices and maps at API boundaries when storing or returning them.
- Do not expose internal mutable maps/slices directly.
- `nil` is a valid empty slice.
- Return `nil` instead of `[]T{}` for empty slices unless serialization semantics require an allocated empty slice.
- Check slice emptiness with `len(s) == 0`, not `s == nil`.
- Use `var s []T` for empty slices that will be appended to.
- Use `make(map[K]V)` for empty maps that will be populated.
- Use map literals for fixed initial contents.
- Provide capacity hints for maps and slices when the expected size is known:

  ```go
  xs := make([]T, 0, len(input))
  m := make(map[K]V, len(input))
  ```

## Channels

- Prefer unbuffered channels or channels with size one.
- Any larger buffer requires a clear reason:
  - why that size is correct,
  - what prevents the channel from filling,
  - and what happens under load.

## Time

- Use `time.Time` for instants.
- Use `time.Duration` for intervals.
- Do not represent time or duration as naked integers unless forced by an external API.
- If an external API requires integers, include the unit in the field name, for example `intervalMillis`.
- For timestamps in strings, prefer RFC3339.
- Use `Time.AddDate` for calendar changes.
- Use `Time.Add` for exact elapsed durations.

## Structs and Embedding

- Avoid embedding types in public structs. It leaks implementation details and constrains future API changes.
- Embed only when it provides a clear semantic benefit and all promoted methods/fields belong on the outer type.
- Do not embed mutexes.
- Embedded fields, when used, belong at the top of the struct with a blank line before regular fields.
- Prefer named fields over embedding for implementation details.
- Use field names when initializing structs:

  ```go
  cfg := Config{
      Addr: "localhost:8080",
  }
  ```

- Omit zero-value fields unless the zero value is meaningful documentation.
- Use `var x T` for a zero-value struct.
- Use `&T{...}` instead of `new(T)` for struct pointers.
- Marshaled structs must have explicit field tags such as `json:"name"`.

## Declarations and Naming

- Use `gofmt` and `goimports`.
- Keep lines reasonably short. Aim for a soft limit around 99 characters.
- Group related imports, constants, variables, and types.
- Do not group unrelated declarations.
- Import grouping should be:
  1. standard library
  2. everything else
- Package names must be lower-case, short, non-plural, and specific.
- Avoid package names like `common`, `util`, `shared`, or `lib`.
- Use MixedCaps for Go names.
- Test names may use underscores for scenario grouping, for example `TestParse_InvalidInput`.
- Avoid import aliases unless:
  - the package name differs from the import path's last element, or
  - there is a direct conflict.
- File organization:
  - type definitions first,
  - constructor functions after the type,
  - methods grouped by receiver,
  - functions in rough call order,
  - helper functions near the end.

## Local Variables and Control Flow

- Use `:=` when declaring and assigning a non-zero explicit value locally.
- Use `var` when the zero value is intentional or clearer.
- Reduce variable scope when it improves readability.
- Do not reduce scope if it causes nesting or obscures control flow.
- Prefer early returns and `continue` to reduce nesting.
- Avoid unnecessary `else` after `return`, `break`, or `continue`.
- Avoid naked boolean or ambiguous parameters at call sites.
- Add inline parameter comments for unclear literals:

  ```go
  printInfo("foo", true /* isLocal */, true /* done */)
  ```

- Prefer typed constants or custom types over naked booleans for public APIs.
- Use raw string literals for strings with quoting or escaping complexity.

## Enums and Constants

- Use custom types with `iota` for enums.
- Start enums at `1` unless the zero value is a useful default.
- Keep constants local unless they are used across functions/files or are part of an external contract.

## Formatting and Printf

- If a format string is declared outside a `Printf`-style call, make it `const` so `go vet` can analyze it.
- Name custom printf-style functions with an `f` suffix, for example `Wrapf`, so vet can check them.

## Performance

- Optimize only when code is on a hot path or measurements justify it.
- Prefer `strconv` over `fmt` for primitive/string conversions.
- Avoid repeated `[]byte("constant")` conversions in loops; convert once and reuse.
- Preallocate slices and maps when the expected size is known.
- Do not sacrifice clarity for hypothetical micro-optimizations.

## Tests

- Prefer table-driven tests when testing the same behavior across multiple inputs.
- Use `tests` for the table and `tt` for each case.
- Use `give` and `want` prefixes for table fields.
- Use subtests with `t.Run`.
- Keep table-test bodies simple.
- Do not use table tests when each case requires complex branching, conditional mocks, or unrelated setup.
- Split complex table tests into separate focused tests.
- In parallel table tests, capture loop variables correctly:

  ```go
  for _, tt := range tests {
      tt := tt
      t.Run(tt.name, func(t *testing.T) {
          t.Parallel()
          // ...
      })
  }
  ```

- Test behavior, not implementation details.
- Prefer narrow, shallow tests with clear assertions.

## Functional Options

Use functional options for constructors or public APIs when optional arguments may grow, especially with three or more parameters.

Preferred pattern:

```go
type options struct {
    cache  bool
    logger *zap.Logger
}

type Option interface {
    apply(*options)
}

type cacheOption bool

func (c cacheOption) apply(opts *options) {
    opts.cache = bool(c)
}

func WithCache(c bool) Option {
    return cacheOption(c)
}
```

Prefer option types with unexported `apply` methods over closure-only options when debuggability, testability, or comparability matters.

## Codex Review Checklist

When generating or reviewing Go code, verify:

- Interfaces are passed as values, not pointers.
- API contracts have compile-time interface checks when useful.
- Errors are handled once and wrapped with useful context.
- No production code uses panic for recoverable errors.
- Only `main` exits the process.
- Goroutines have shutdown and wait paths.
- Slices/maps are copied at ownership boundaries.
- Mutexes are named fields, not embedded.
- Time uses `time.Time` and `time.Duration`.
- Struct literals use field names.
- Marshaled structs have explicit tags.
- Package names are short, lower-case, singular, and specific.
- Imports are goimports-formatted.
- Tests are focused and not over-abstracted.
- Hot-path code avoids obvious allocation/conversion waste.
- The final code passes formatting, tests, vet, and configured linters.
