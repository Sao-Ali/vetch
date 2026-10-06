# Contributing to Vetch

Keep each pull request focused enough to review on its own. The root
[README](README.md) explains the project, and the [development guide](docs/development.md)
covers local setup.

## Choose the work

Check existing issues before starting. For a behavior change, new API field, or
larger piece of work, use an issue to agree on scope and acceptance criteria.
Small documentation and typo fixes can go straight to a pull request. If an
issue has a design decision, follow it or explain the change in the PR.

## Create a branch

Start from an up-to-date `main` when your working tree is clean:

```bash
git switch main
git pull --ff-only origin main
git switch -c issue-22-vm-api
```

Use a short branch name such as `issue-22-vm-api` when linked to an issue, or
`docs/contributing-guide` for work without one. If you have already made local
changes on `main`, create the branch before committing with
`git switch -c issue-22-vm-api`; keep those changes intact.

## Make and verify the change

- Keep the PR within the agreed scope. Add or update tests for behavior changes.
- For API fields or Kubebuilder markers, run `make manifests generate` and
  include the generated CRD and DeepCopy changes. Do not edit generated files
  directly.
- For Go changes, run `make lint-fix` and `make test`. CI also runs `make lint`
  and checks that generation leaves no uncommitted changes.
- For documentation-only changes, check links and run `git diff --check`.
- Document behavior or compatibility changes, including anything reviewers
  should know about migration or limitations.

Before committing, review `git diff` and `git status --short`. Do not include
local binaries, coverage files, kubeconfigs, credentials, tokens, or secrets.

## Open a pull request

Stage only the intended files, inspect the staged diff, then commit and push.
For example:

```bash
git add -p
git diff --cached
git commit -m "Add VM API fields"
git push -u origin issue-22-vm-api
```

Stage new files by name with `git add` as well. Open a PR against `main` using
GitHub or `gh pr create`, and fill in the
[PR template](.github/pull_request_template.md). A useful PR description
answers four questions:

1. What problem or issue does this address?
2. What changed, and why was this approach chosen?
3. How was it verified? List commands and any manual checks.
4. What behavior, compatibility, or follow-up work should reviewers notice?

Use `Closes #22` in the PR description only when the PR fully resolves issue
22. Use `Refs #22` for partial work or related context. GitHub closes an issue
when a PR with a closing keyword merges into the default branch. See
[GitHub's issue linking guide](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue).

For example, a PR for the VM API could say:

```text
Title: Add VirtualInferenceCluster VM fields (#22)
Context: Closes #22. The API needs to accept a VM request before VM creation is added.
Changes: Add VM and worker fields, validation, and generated CRD/DeepCopy code.
Validation: make manifests generate; make lint-fix; make test.
Review notes: VM requests report provisioning pending; this PR creates no VM.
```

Open a draft PR if you want feedback before the work is ready. When ready,
request review, let CI finish, and address review comments on the same branch.
The author and reviewer should confirm that the PR matches its stated scope and
that the checks pass before merging. Deploying or changing a cluster is a
separate action from opening or merging a PR.
