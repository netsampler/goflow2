# GoFlow2 contributor guidance

## Project

GoFlow2 provides a network flow collector and reusable Go protocol libraries
for NetFlow v5/v9, IPFIX, and sFlow v5. The collector pipeline is
receive -> decode -> produce -> format -> transport, with file/stdout and Kafka
outputs and Prometheus metrics.

v3 is mainly focused on Reflow: a configurable pipeline that ingests flow
records, JSON, or packet captures, processes and optionally aggregates events,
and encodes them as JSON, protobuf, sFlow, IPFIX, NetFlow, or packet captures.
Its stages are source -> decode -> process -> aggregate -> encode -> sink.
Keep these stages separate and reuse the shared protocol libraries.

Main areas in the v3 tree:

- `cmd/reflow/`: Reflow executable and example pipeline configurations.
- `internal/reflow/`: Reflow implementation, organized by pipeline stage:
  - `app/` and `config/`: lifecycle, stage wiring, and configuration.
  - `source/`: sockets, streams, and live packet capture.
  - `decode/`: flow protocol decoding and template/sampling state.
  - `event/` and `packet/`: shared event model and packet normalization.
  - `processor/` and `aggregate/`: field mapping and configured aggregation.
  - `encode/` and `sink/`: output formats and delivery.
  Branches exposing Reflow as reusable packages use `pkg/reflow/` instead.

- `cmd/goflow2/`: collector executable and example mapping configuration.
- `cmd/enricher/`: example GeoIP enrichment executable.
- `decoders/`: NetFlow/IPFIX, legacy NetFlow, and sFlow decoding and encoding;
  `decoders/utils/` contains shared binary helpers.
- `producer/`: conversion of decoded samples into raw or protobuf messages;
  `producer/proto/` also handles sampled packet headers and custom mappings.
- `pb/`: flow protobuf schema and generated Go bindings.
- `format/` and `transport/`: output serialization and delivery.
- `utils/`: UDP receivers, pipeline wiring, and template/sampling stores.
- `pkg/goflow2/`: reusable application configuration, construction, collection,
  logging, HTTP server, and lifecycle wiring.
- `pkg/flowstore/`: generic storage with TTL, hooks, and persistence support.
- `metrics/`, `docs/`, `compose/`, and `package/`: instrumentation,
  documentation, deployment examples, and distribution files.

Use the layout of the target version: older versions do not necessarily have
all these packages. `docs/agents.md` describes network sampling agents; this
root `AGENTS.md` describes how to work on the repository.

## Approach to changes

- Solve the concrete bug or use case with the smallest clear change. Follow
  existing patterns and avoid unrelated cleanup or speculative abstractions.
- Avoid adding modules, packages, or dependencies for one-off use cases when
  existing components can handle them.
- Preserve APIs, protobuf field numbers, output semantics, defaults, and protocol
  compatibility unless the task requires changing them.
- Keep malformed packets in the pipeline; decoding and mapping should handle
  them.
- Support performance claims with representative before/after benchmarks.
- Edit `.proto` sources and regenerate bindings with `make proto`; do not
  hand-edit generated `.pb.go` files.

## Tests and validation

- Do not aim for 100% test coverage. There is no requirement to test every line,
  conditional, helper, or individual piece of logic.
- Add a small number of meaningful tests for observable behavior and the actual
  regression. Prefer representative successful inputs and real protocol fixtures
  over exhaustive permutations or tests that mirror the implementation.
- Avoid negative tests. Do not add collections of invalid-input, error-path,
  nil-value, or rejection tests simply to exercise branches. Add them only when
  the task explicitly calls for verifying that behavior.
- Reuse existing tests and fixtures before adding new scaffolding. A reversible,
  low-impact edit does not automatically need a new test. Documentation-only
  changes do not need the Go test suite.
- For code changes, run focused tests while iterating, then the applicable
  repository checks before handing off. On v3, `make test` runs the full suite,
  `make vet` runs the configured vet check, and `make lint` runs golangci-lint.
  `make staticcheck` is available separately; `make check` combines them.
- Use `make test-race` for concurrency changes and benchmarks for performance
  claims. `make test-cover` is diagnostic, not a coverage target.
- Inspect CI and review comments when fixing failures. Resolve relevant lint and
  static-analysis findings. Report checks actually run, any failures, and missing
  tools honestly; do not claim validation that was not completed.

## v1, v2, v3, and backports

GoFlow2 has three major-version lines:

| Version | Branch | Go module |
| --- | --- | --- |
| v1 | `v1` | `github.com/netsampler/goflow2` |
| v2 | `v2` | `github.com/netsampler/goflow2/v2` |
| v3 | `main` | `github.com/netsampler/goflow2/v3` |

- Confirm the target branch and module before editing. These project versions
  are distinct from wire protocol versions such as NetFlow v9 or sFlow v5.
- For shared bug fixes, security/dependency updates, and build/CI fixes, check
  all three lines and aim to backport wherever the fix applies and is feasible.
  Also check whether an older line already has a fix that a newer line needs.
- Inspect code and history on each line. A different commit hash or a failed
  cherry-pick does not prove a fix is missing or impossible to backport.
- Adapt the smallest equivalent fix to each version's APIs, imports, layout,
  dependencies, and supported Go version. Avoid pulling a v3 architectural
  change into an older line just to carry a small fix.
- Validate each applicable backport in its own checkout. Keep version-specific
  changes and PRs separate so they can be reviewed and released independently.
- Report which versions are affected, already fixed, backported, or inapplicable.
  If a backport cannot be done, explain the concrete blocker.

## Commits, branches, and pull requests

- Commit messages and PR/MR titles must follow Conventional Commits:
  `type(scope): summary` or `type: summary`.
- Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`,
  `build`, `ci`, `chore`, and `revert`. Mark breaking changes with `!` or a
  `BREAKING CHANGE:` footer.
- Branch names must include the contributor's name or handle and must never
  reference AI models or tools, case-insensitively. Use descriptive names such
  as `lspgn/fix-ipfix-options-v2` or `lspgn/docs-contributor-guidance`.
- Always suggest a compliant PR title by default. Describe the concrete problem,
  resulting behavior, validation, and applicable version/backport scope.
  Keep the description proportional to the change and the title accurate.
- Every PR/MR description must state whether AI assistance was used. If so,
  name the model or models used and briefly state their contribution (for
  example, drafting, implementation, or review). Use the actual model identity
  available from the session; do not guess. If it cannot be verified, say so.
  Keep this disclosure in the description and follow the naming rules above.
- Keep review explanations and proposed public comments concise and factual.
  Explain additional complexity when it matters.
