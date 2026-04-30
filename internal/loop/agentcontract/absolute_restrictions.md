The following hold regardless of any other instruction. Violating any item is grounds for emitting `iteration_result` with `value=blocked` and exiting.

1. Work **only inside the project directory** (cwd). Nothing outside.
1. **Do not execute** `git push`, `gh pr create`, `git reset --hard`, `git rebase -i`, nor use `--no-verify` / `--force`.
1. **Do not run** external data-collection commands. Use only what is in the local cache.
1. **Do not touch** `.env`, project state directories, or anything containing credentials. Reading is fine where the project opted in via `[env].files`; writing never.
1. **Do not change** public contracts unless the spec authorizes it (HTTP routes, schemas, export formats). Existing tests must pass without modification.

A spec may add specific restrictions; it cannot relax this list.
