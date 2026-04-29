---
title: "Template: ADR"
sidebar_position: 4
---

# Template: Architecture Decision Record

Copy the content below to create a new ADR at `docs/adrs/YYYY-MM-DD-title-slug.md`.

Use the creation date as prefix (e.g., `2026-04-29-adopt-cobra.md`).

---

````markdown
---
title: "Decision title"
type: adr
status: draft
authors:
  - Your Name
reviewers: []
created: YYYY-MM-DD
decision-date:
superseded-by:
supersedes:
review-by:            # next review date (default: 6 months after approval)
tags:
  - adr
comments: true
---

# Decision title

## Context

Describe the context and the problem that motivated this decision. Include:

- What current situation prompts the need to decide?
- Which forces or constraints influence the decision? (technical, organizational, business)
- What alternatives were considered?

## Decision

Describe the decision clearly and directly. Use active voice ("We adopt...", "We will use...", "We replace...").

If relevant, include:

- What concretely changes
- How it will be implemented
- Which constraints or guardrails accompany the decision

## Consequences

### Positive

- What improves with this decision

### Negative

- What gets worse or more complex
- What trade-offs were accepted

### Risks

- Identified risks and their mitigations
````

## Rules for ADRs

- Approved ADRs are **immutable**. Typo fixes are accepted; substantive changes are not.
- To change a decision, create a **new ADR** that supersedes the previous one (`supersedes` field).
- The previous ADR receives `superseded-by` pointing to the new one and is eventually archived.
- Use objective language. The ADR should be understandable by someone who did not participate in the discussion.
