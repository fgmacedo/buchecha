---
title: "Template: PRD"
sidebar_position: 2
---

# Template: Product Requirements Document

Copy the content below to create a new PRD at `docs/specs/YYYY-MM-DD-title-slug.md` (standalone) or `docs/specs/<initiative>/YYYY-MM-DD-title-slug.md` (inside an initiative).

PRDs define the **problem** and **business requirements** before discussing solutions. The primary audience is product, design, and business, with engineering as a stakeholder.

---

````markdown
---
title: "PRD title"
type: prd
status: draft
authors:
  - Your Name
reviewers: []
created: YYYY-MM-DD
decision-date:
superseded-by:
supersedes:
review-by:
area:                  # optional
team:                  # optional
tags: []
comments: true
---

# PRD title

## Summary

One to three sentences explaining what we want to solve and for whom.

## Problem

### Context

What is the current situation? What is happening that motivates this initiative?

### User pain

Who is affected and how? Include evidence when possible (metrics, feedback, support tickets).

### Impact of not solving

What happens if we do nothing?

## Goals

### What we want to achieve

- [ ] Goal 1
- [ ] Goal 2

### Success metrics

How will we know we solved the problem?

| Metric | Current | Target |
|---|---|---|
| ... | ... | ... |

### Non-goals

What is **out of scope** for this initiative (and why).

- ...

## Audience

Who are the affected users? Segment if needed.

| Segment | Description | Estimated volume |
|---|---|---|
| ... | ... | ... |

## Requirements

### Functional

What the system/product must do:

- [ ] FR1: ...
- [ ] FR2: ...

### Non-functional

Performance, security, accessibility, compliance constraints:

- [ ] NFR1: ...

## Expected experience

Describe the high-level user experience. May include:

- User stories ("As [persona], I want [action] so that [benefit]")
- Simplified flows
- Wireframes or mockups (link or image)

## Constraints and dependencies

- Known technical, legal, or business constraints
- Dependencies on other teams or systems
- Relevant deadlines

## Alternatives considered

If applicable, what other approaches were evaluated?

| Alternative | Pros | Cons |
|---|---|---|
| ... | ... | ... |

## Open questions

Points still requiring alignment or investigation:

- [ ] ...

## References

- Links to research, tickets, related documents, ADRs, etc.
````

## Tips

- **Focus on the problem, not the solution.** The technical solution lives in the Spec. The PRD aligns "what" and "why".
- **Be specific with metrics.** "Improve experience" is not measurable. "Reduce onboarding time from 3 days to 1 day" is.
- **Include what is out of scope.** Prevents scope creep and aligns expectations before starting.
- **Inside an initiative**: place the PRD inside the corresponding subdirectory.
