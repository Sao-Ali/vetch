# Vetch documentation

This directory contains the detailed design and project guidance for Vetch.

| Document | Purpose |
| --- | --- |
| [Architecture](architecture.md) | Target system design, component responsibilities, and worker semantics |
| [Roadmap](roadmap.md) | Project milestones, success criteria, and final demonstration |
| [Evaluation](evaluation.md) | Cloud/edge benchmark and routing evaluation plan |
| [Development](development.md) | Local setup, verification commands, and repository layout |
| [Local inference](local-inference.md) | Implemented VM/agent/gateway path and dedicated-cluster setup |
| [Validation](local-inference-validation.md) | Recorded live inference and repository verification |
| [Cloud completion plan](cloud-completion-plan.md) | Accepted self-hosted direction and remaining host acceptance gate |
| [Remote demo and edge testing](edge-device-testing-plan.md) | Next MacBook SSH demonstration and planned physical-board work |
| [MacBook SSH setup](macbook-ssh-demo.md) | This host's addresses, username, key enrollment, connection steps, and tunnel troubleshooting |

## Source of truth

The 2026–2027 *Vetch Full Proposal* and *Vetch Advisor Overview: Inference
Infrastructure* provide the original direction and deliverables. The accepted
[cloud completion plan](cloud-completion-plan.md) refines deployment to this
self-hosted central machine, with AWS optional and edge integration following
local inference. The proposals' broader content is consolidated into these
version-controlled documents. The source
PDFs remain local and are ignored by Git.

When project materials disagree, use this order:

1. The accepted self-hosted plan and subsequent explicit decisions for intended direction.
2. The checked-in API, controller, and tests for behavior that exists today.
3. The original proposals and older design notes where they do not conflict
   with the accepted refinement or implemented behavior.

Planned components are identified as planned and must not be presented as
current capabilities.
