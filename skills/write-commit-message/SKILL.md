---
name: write-commit-message
description: Draft Conventional Commit messages for Vetch from the actual staged or working-tree changes. Use when a contributor asks for a commit title or body; do not stage, commit, amend, or push.
metadata:
  source: https://github.com/github/awesome-copilot/tree/main/skills/git-commit
  license: MIT
---

# Write Commit Message

Propose a message that describes the change being committed, not the conversation
that produced it.

## Determine the candidate change

Run `git status --short`. If the index contains changes, inspect
`git diff --cached --stat` and `git diff --cached`. Treat that staged set as the
commit. Mention relevant unstaged or untracked files separately.

If nothing is staged, inspect tracked changes with `git diff` and review the names
of untracked, non-ignored files. Read untracked source files relevant to the proposed
commit because `git diff` cannot show them. If unrelated changes are mixed together,
recommend separate commits and provide a message for each logical group.

Do not inspect ignored local notes, kubeconfigs, environment files, keys, tokens,
or credential directories. Never reproduce a suspected secret in the response.

## Format

Use Conventional Commits:

```text
<type>[optional scope][optional !]: <imperative description>

[optional body explaining motivation and material behavior]

[optional BREAKING CHANGE footer or issue reference]
```

Choose the type from the actual diff: `feat`, `fix`, `docs`, `test`, `refactor`,
`build`, `ci`, `perf`, `style`, `chore`, or `revert`. Prefer meaningful Vetch scopes
such as `api`, `controller`, `config`, `docs`, `ci`, or `skills`; omit a scope when
the change spans the repository. Keep the subject specific and reasonably short.

Return the recommended message in a code block. Add a brief split recommendation
only when the candidate change contains multiple independent concerns.

This skill drafts text only. Never run `git add`, `git commit`, `git reset`,
`git push`, amend a commit, or bypass hooks.

Adapted from GitHub's MIT-licensed `git-commit` skill in `awesome-copilot`.
