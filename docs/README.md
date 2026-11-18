# Vetch documentation

This directory contains the detailed design and project guidance for Vetch.

| Document | Purpose |
| --- | --- |
| [Architecture](architecture.md) | Target system design, component responsibilities, and worker semantics |
| [Roadmap](roadmap.md) | Project milestones, success criteria, and final demonstration |
| [Evaluation](evaluation.md) | Cloud/edge benchmark and routing evaluation plan |
| [Development](development.md) | Local setup, verification commands, and repository layout |

## Source of truth

The 2026–2027 *Vetch Full Proposal* and *Vetch Advisor Overview: Inference
Infrastructure* define the intended product direction and deliverables. Their
content is consolidated into these version-controlled documents. The source
PDFs remain local and are ignored by Git.

When project materials disagree, use this order:

1. The two proposal documents for intended direction and deliverables.
2. The checked-in API, controller, and tests for behavior that exists today.
3. Older design notes only when they do not conflict with either source.

Planned components are identified as planned and must not be presented as
current capabilities.
