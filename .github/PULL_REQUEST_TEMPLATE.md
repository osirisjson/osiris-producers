# What does this PR change?

## Why is it needed?
<!-- Link the issue/discussion this follows, or explain why none exists. -->

## How was it tested?
Explain briefly how and what you tested.

## Change type
- [ ] Editorial (docs/comments only, no behavior change)
- [ ] Additive (new field/resource type/flag, no breaking change)
- [ ] Breaking (changes the shape or semantics of existing output)

## Checklist
- [ ] I linked the relevant issue/discussion (or explained why none exists)
- [ ] I kept scope focused and avoided unrelated changes
- [ ] I added/updated tests (if applicable)
- [ ] I ran `go vet ./...`, `go build ./...`, and `go test ./...`
- [ ] I updated the affected producer's `CHANGELOG.md` and the root
      `CHANGELOG.md` (if behavior changed)
- [ ] I bumped `generatorVersion` in the producer source to match the
      `CHANGELOG.md` entry (if this is a release-bound change)
- [ ] Commit messages follow Conventional Commits (`feat`, `fix`, `docs`,
      `chore`, `test`, `refactor`)
- [ ] I MUST NOT include real production data (IPs, hostnames, credentials,
      serial numbers etc.) in code, tests, or docs

## Breaking changes
<!-- If none, write "None". Otherwise describe what consumers must change. -->
