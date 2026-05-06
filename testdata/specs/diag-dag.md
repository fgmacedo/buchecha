# Diag DAG spec

A cooperative-spec fixture for exercising the loop end to end. It produces a DAG with one parallelizable phase and one dependent phase so the dashboard has non-trivial structure to render. All tasks write small marker files under a fixture directory so the work is mechanical and predictable.

The fixture directory is `testdata/diag-dag-output/` relative to the project root. Every task creates exactly one file there.

## P1: parallel writers

Three independent tasks. They share no inputs and can run in any order or in parallel.

### T1.1: write alpha

Create `testdata/diag-dag-output/alpha.txt` with the literal content `alpha`.
Acceptance: file exists with that exact content.

### T1.2: write beta

Create `testdata/diag-dag-output/beta.txt` with the literal content `beta`.
Acceptance: file exists with that exact content.

### T1.3: write gamma

Create `testdata/diag-dag-output/gamma.txt` with the literal content `gamma`.
Acceptance: file exists with that exact content.

## P2: aggregate (depends on P1)

Reads the three markers from P1 and produces a combined file. Cannot start until every P1 task is done.

### T2.1: combine

Concatenate the contents of `alpha.txt`, `beta.txt`, and `gamma.txt` (in that order, one per line) into `testdata/diag-dag-output/combined.txt`.
Acceptance: `combined.txt` exists and contains exactly:

```
alpha
beta
gamma
```

### T2.2: write summary

Create `testdata/diag-dag-output/summary.txt` containing the literal line `3 markers combined`.
Acceptance: file exists with that exact content.
