---
name: release-store
description: Preview and publish semantic-version releases for the selfstore Go CLI through its Taskfile and GitHub Actions workflow. Use when the user asks to release this repository.
user-invocable: true
disable-model-invocation: true
---

# Release Store

Use this workflow only in the selfstore repository. The release task creates
and pushes an annotated version tag; that tag starts the GitHub Actions
workflow, which publishes the GitHub release and updates the Homebrew tap.

## Workflow

1. **Verify the repository and release state**

   Confirm the repository contains `Taskfile.yml` and `scripts/release.sh`.
   Run:

   ```sh
   git rev-parse --show-toplevel
   git branch --show-current
   git status --porcelain
   ```

   Release only from `main` with an empty porcelain status. If either condition
   fails, stop and report the blocker. Do not switch branches, stash, commit,
   push `main`, or discard changes automatically.

2. **Choose the version increment**

   Use the increment the user named: `patch`, `minor`, or `major`. If they did
   not specify one, ask them to choose. Do not infer the increment from the
   change or choose one on their behalf.

3. **Preview the release**

   Run the documented dry-run for the selected increment:

   ```sh
   task release:<increment> -- --dry-run
   ```

   For example, `task release:minor -- --dry-run`. The script fetches `main`
   and tags, verifies local `main` matches `origin/main`, checks the next stable
   semantic version, and runs `task check` and `task release:check`. It must
   finish successfully and print the proposed tag. If validation fails, stop;
   do not bypass checks.

4. **Publish the selected release**

   When the user explicitly asked to release and selected the increment, run:

   ```sh
   task release:<increment>
   ```

   The command repeats validation, creates an annotated `vMAJOR.MINOR.PATCH`
   tag, and pushes only that tag. A successful tag push starts GitHub Actions;
   it does not mean the release has finished publishing.

5. **Verify and report**

   Check the command output, then run:

   ```sh
   git status --porcelain
   git log -5 --oneline --decorate
   git ls-remote --tags origin refs/tags/<tag>
   ```

   Confirm the tag exists on `origin` before reporting that it was pushed.
   When GitHub read tools are available, check the release workflow and
   GitHub Release for that tag. Otherwise, report that publication is pending
   rather than claiming the release is available. Report the validation
   results and whether the working tree is clean.

## Failure handling

- If the branch is not `main`, the tree is dirty, or local `main` is not
  synchronized with `origin/main`, stop and explain what needs to be resolved.
- If tag pushing fails, the tag may remain local. Inspect both local and
  remote tag state before any retry; never force-push or move a release tag.
- If GitHub Actions fails after the tag is pushed, inspect the workflow run.
  Retry a transient workflow failure only when appropriate. A code or workflow
  fix requires a new version tag; do not reuse or replace the failed release
  tag.
