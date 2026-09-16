---
name: explain-vetch
description: Explain the Vetch repository, its current architecture, controller flow, project layout, conventions, and safe development commands. Use for contributor onboarding or questions about where behavior lives; do not use for implementing changes.
metadata:
  source: https://github.com/quokkify/skills/tree/main/skills/repository/codebase-onboarding
  license: MIT
---

# Explain Vetch

Build an evidence-based map of the repository as it exists now.

## Inspect

Read `AGENTS.md` first. Then inspect only the files needed to establish:

- the implemented milestone and explicitly planned future work;
- the API group, version, kind, schema, and generated artifacts;
- the path from a custom resource event through `cmd/main.go` to reconciliation;
- local development, generation, lint, test, build, and deployment commands;
- files that are generated or managed by Kubebuilder;
- external systems that are active today versus merely planned.

Use `README.md`, `PROJECT`, `go.mod`, `Makefile`, `api/`, `cmd/`,
`internal/controller/`, `config/`, and `.github/workflows/` as primary evidence.
Inspect Git history only when it exists and the question needs it.

Never read or summarize `.local/`, kubeconfigs, credential directories, environment
files, keys, or secret values. Do not infer active features from dependencies alone.

## Explain

Give a concise answer appropriate to the question. When a full walkthrough is
requested, cover:

1. Purpose and current capabilities.
2. Current request/reconciliation flow.
3. Important directories and entry points.
4. Generated files and contributor boundaries.
5. Fast and full verification commands.
6. The correct starting file for common changes.

Cite concrete repository paths. Clearly label planned architecture as planned and
unknowns as unknown. Do not claim that ConfigMaps, KubeVirt VMs, inference,
accelerators, or cloud deployment exist unless the current code proves it.

This skill is read-only. Do not edit files, mutate a Kubernetes cluster, stage
changes, commit, or push while explaining the repository.

Adapted from the MIT-licensed `codebase-onboarding` skill by affaan-m/ECC,
distributed through quokkify/skills.
