# Contributing to pev

Thanks for considering a contribution! Start with [CLAUDE.md](CLAUDE.md) — it has the architecture map, conventions, and the authoring checklists for new checks and primitives.

## Before you open a PR

- [ ] `make test` passes locally (`go test ./... -race -shuffle=on`)
- [ ] `make lint` passes (`golangci-lint`)
- [ ] If you changed YAML packs, `make e2e` exercises the relevant container OS
- [ ] PR title uses [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`

## Commit style

The changelog is auto-generated from PR titles. Use:

- `feat: ...` — new check, primitive, or capability
- `fix: ...` — bug or correctness fix
- `docs: ...` — documentation only
- `refactor: ...` — internal cleanup, no behavior change
- `chore: ...` — tooling, CI, dependencies

Append `!` for breaking changes (e.g. `feat!: bump SchemaVersion to 2`).

## Code review expectations

`internal/checks/`, `internal/primitives/`, and `checks/` require an extra reviewer (see `CODEOWNERS`). Cosmetic fixes elsewhere can land with one review.

## Adding a new check

See CLAUDE.md §5. Quick path:

1. Pick `id` in `<area>.<topic>.<facet>` form.
2. Add YAML under `checks/<area>/`.
3. Add references to Posit docs (the docs.posit.co AI chat assistant is a good way to find the right page).
4. Add a row to `docs/runbook-mapping.md` if it implements a runbook prereq.

## Adding a new primitive

See CLAUDE.md §6. Quick path:

1. New file in `internal/primitives/<name>.go`.
2. Register in the file's `init()` with allowed `with:` keys.
3. Document in `docs/primitives.md`.
4. Positive + negative tests in `internal/primitives/primitives_test.go`.

## Reporting security issues

See [SECURITY.md](SECURITY.md). Use GitHub Security Advisories for private disclosure.

## License

pev is licensed under the [Apache License, Version 2.0](LICENSE). By submitting a contribution you agree it is licensed under the same terms.
