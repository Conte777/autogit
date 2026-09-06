# Cutting a Release

A release is a tag. `.github/workflows/release.yml` fires on `v*`, and goreleaser builds the archives, writes the GitHub release and moves the Homebrew cask in `Conte777/homebrew-tap`.

## The plugin version is the tag

`plugins/autogit/.claude-plugin/plugin.json` always carries the version of the release it ships under, whether or not a command, a hook or the MCP wiring moved. Two gates hold it:

- `TestPluginVersionIsNotBehindTheNewestTag` fails any pull request whose manifest is behind the newest tag — the forgotten bump, caught before the tag exists.
- The release workflow compares the manifest against `${GITHUB_REF_NAME#v}` and refuses to publish when they differ. It runs before any artifact is uploaded and before the cask moves.

The order matters, and it is the one thing neither gate can enforce. `/plugin marketplace add Conte777/autogit` installs from the **default branch**, not from the tag, so between a merged bump and the tag itself `main` advertises a version nobody can install. Keep that window short: merge the bump, then tag the merge commit.

## The sequence

```sh
# 1. Bump the manifest to the version about to ship, on its own branch.
#    docs/agents/code-change-workflow.md applies: this is a change to code.
gh pr create --title "build: release v0.3.0" --body "..."

# 2. Merge it, then tag the merge commit.
gh pr merge <n> --merge --delete-branch
git pull --ff-only
git tag -a v0.3.0 -m "v0.3.0"
git push origin v0.3.0

# 3. Watch the release, then confirm what shipped.
gh run watch
gh release view v0.3.0 --json assets --jq '.assets[].name'
```

Pick the number by what landed since the last tag, per Conventional Commits: a `feat` takes the minor, a `fix` or a `build` alone takes the patch.

## Two things the gate makes harder

**Re-running a published tag.** `.goreleaser.yaml` is configured for idempotent re-runs (`replace_existing_artifacts`, `mode: replace`), but a re-run of a tag cut before this policy will abort on the version check, because that tag's tree carries the old manifest. Move the tag onto a commit whose manifest matches, or leave the release as it shipped.

**Prereleases.** The trigger is `tags: ["v*"]` and the check wants a plain `major.minor.patch`, so `v0.3.0-rc.1` fails it. That is deliberate: the manifest would have to say `0.3.0-rc.1` on the default branch, and every marketplace user would be served a release candidate. If prereleases are ever wanted, decide what the plugin should advertise during one before loosening the check.
