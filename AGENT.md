# Agent guide

## Project purpose

`resumer` discovers local AI coding-agent sessions, presents them in a searchable picker or list, and resumes the selected session with that agent's native CLI. Keep changes focused on local session discovery, identification, and resume behavior.

## Core constraints

- Do not add token, cache-hit, cost, or other usage analytics unless explicitly requested. The session model and output should contain only information useful for finding or resuming a session.
- Providers are read-only: scan local agent data and never install hooks, modify agent configuration, or upload session contents.
- A disabled provider must not scan or parse its data. Preserve the existing provider registry/config behavior.
- Validate storage formats and resume commands against the agent's actual source or authoritative CLI reference. Third-party trackers are useful research references, not substitutes for provider-source verification.
- Keep test fixtures synthetic. Do not commit real session logs, prompts, home-directory paths, credentials, or other personal data.

## Adding a provider

1. Add a package under `internal/provider/<name>` implementing `provider.Provider` (`Name`, `Badge`, `BadgeANSI`, `IsAvailable`, `ListSessions`, `LoadDetail`).
2. Parse only the metadata required by `session.Session`; avoid retaining full conversation content beyond prompt fields already used by the picker.
3. Register the provider in `internal/cli/cli.go`, add a resume command and provider-specific cwd handling if required, and provide an install/help link.
4. Add synthetic fixtures and parser tests for titles, prompts, timestamps, filters, missing/malformed records, and resume arguments.
5. Add the provider to the README table and any relevant picker badge styles. Provider enable/disable should continue to work through the registry without special-case configuration logic.

## Validation

Run before submitting changes:

```bash
go fmt ./...
go test ./...
go vet ./...
git diff --check
```

Keep the project compatible with the Go version declared in `go.mod` and prefer existing dependencies and conventions.
