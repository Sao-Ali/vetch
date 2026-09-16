---
name: verify-before-commit
description: Verify Vetch changes before a commit or pull request using repository checks, generated-file consistency, manifest rendering, and secret hygiene. Use when asked whether changes are ready to commit; report evidence without committing or pushing.
metadata:
  source: https://github.com/obra/superpowers/tree/main/skills/verification-before-completion
  license: MIT
---

# Verify Before Commit

Run fresh checks and base every readiness claim on their output.

## Establish scope

Read `AGENTS.md`, `README.md`, `Makefile`, `git status --short`, and the candidate
diff. If changes are staged, verify the staged set while still reporting unstaged
and untracked files. If nothing is staged, verify all non-ignored working-tree
changes. Never inspect `.local/` or credential contents.

## Repository gate

Run the checks that apply to the candidate change:

1. `git diff --check` for tracked patches.
2. `make manifests generate` when API types, controller RBAC markers, or generated
   files may be affected. Afterward, report any resulting generated changes.
3. `make lint` for Go, generation, build configuration, or CI changes.
4. `make test` for Go, API, controller, generation, or test changes.
5. `make build` when executable Go code or build inputs changed.
6. Render `config/crd`, `config/samples`, and `config/default` with
   `bin/kustomize build <path>` when Kubernetes manifests or their inputs changed.
7. Run `go mod tidy -diff` when `go.mod`, `go.sum`, or imports changed.

For documentation-only or skill-only changes, do not run unrelated expensive checks.
Validate every changed skill with:

```bash
python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-creator/scripts/quick_validate.py" <skill-directory>
```

If that validator is unavailable on another machine, mark skill validation skipped;
do not call it passed.

## Secret and publication hygiene

List the candidate file names with Git. Confirm ignored local outputs and `.local/`
are not candidates. If `gitleaks` is installed, run it against the candidate state.
Otherwise perform a conservative filename and credential-pattern scan without
printing matched secret values. Treat findings as review items, not proof of a leak.
Explicitly report when a dedicated secret scanner was unavailable.

Check that generated binaries, coverage reports, kubeconfigs, `.env` files,
private keys, and credential files are excluded.

## Runtime checks

Only require a live Kind check when the change affects behavior that envtest cannot
prove, such as Kubernetes garbage collection or an end-to-end controller flow.
Confirm the exact context first and use an isolated Kind cluster. Do not create,
modify, or delete cluster resources without authorization. A skipped runtime check
must remain `SKIPPED`, with the reason.

## Report

Return a table of `PASS`, `FAIL`, and `SKIPPED` checks with the command or evidence.
List blocking failures first, then remaining review items. State "ready to commit"
only when all required checks passed and the candidate file set contains no known
secret or unrelated local file.

Never stage, commit, amend, push, deploy, or bypass hooks as part of verification.

Adapted from the MIT-licensed `verification-before-completion` skill by obra.
