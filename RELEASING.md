# Releasing

Two repositories, version-coupled, released in a fixed order. Nothing here was
written down before 2026-08-06, which is why it is now.

## The order, and why it is not arbitrary

```
1.  this repo (truenas-go)      mise run release      → tag vX.Y.Z, push
2.  terraform-provider-truenas  bump go.mod replace   → to that tag
3.  terraform-provider-truenas  mise run release      → tag vA.B.C, push
```

**This library is released first, always.** It has no dependency on the
provider; the provider depends on it.

The provider consumes this library through a `replace` directive, so a provider
release referencing a tag that does not exist yet produces a build which cannot
be reproduced from a clean checkout. The provider's
`.mise/tasks/check-versions` enforces this: with `CHECK_CLIENT_TAG=1` it
verifies the `replace` target is a real tag here.

**A published Go module version is permanent.** Once any proxy has fetched it,
it is cached and cannot be changed or withdrawn. That is why the release task
confirms rather than assumes.

This repo keeps upstream's module path (`module github.com/deevus/truenas-go`)
even though it lives at `cuttlefisch/truenas-go`. Go resolves a `replace` to a
different repository regardless, which is what lets the provider consume this
fork without changing a single import path — and what keeps both repos
mergeable back to `deevus`. Do not rename the module.

## Cutting a release

```sh
mise run release            # computes the version from conventional commits
mise run release 0.17.0     # or state it explicitly
```

The task is **Check → Plan → Confirm → Apply**. It refuses to run off `main`,
with a dirty tree, out of sync with `origin`, or with a failing `mise run ci`.
It shows the release notes, then requires typing `yes`. Nothing unreleased
exits without prompting.

It creates a commit and a **signed annotated tag**, then prints the push
command. It does not push — that step stays deliberate.

### Where the version number comes from

`git-cliff --bumped-version`, computed from conventional commits: `feat` →
minor, everything else → patch. `[bump] breaking_always_bump_major = false` in
`cliff.toml` means a `!` commit bumps the minor rather than cutting 1.0.0,
because we are deliberately on 0.x and a one-way door should not be opened by a
commit message.

**The computed number is a suggestion.** Commit-type inference gets things
wrong — `build(deps): consume the maintained truenas-go fork` swapped the entire
client library and reads as a patch. Override it when it is wrong; that is what
the explicit argument and the confirmation step are for.

## Versioning policy

**0.x while the API surface is still growing.** Six namespaces are still to
land — `sharing.smb`, `user`, `group`, `service`, `certificate`, filesystem
ACLs — and each is a chance to find the shape was wrong. `[bump]
breaking_always_bump_major = false` in `cliff.toml` keeps a `!` commit from
cutting 1.0.0 by accident.

Cut **1.0.0** once those namespaces have shipped and their shapes have survived
contact with a real appliance, and flip `breaking_always_bump_major` to `true`
in the same change.

## Historical tag inconsistency

Eleven of the twelve tags here are lightweight; only `v0.6.0` is annotated, and
none before it was signed. Consistency starts from the next release rather than
by rewriting published tags — anyone may have pinned one.
