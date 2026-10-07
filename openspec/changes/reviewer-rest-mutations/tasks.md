# Tasks: Reviewer REST mutations + robust approval detection

## Implementation

- [x] Add REST helpers (`_gh_api`, `add_pr_labels`, `assign_pr`, `set_pr_body`, `create_issue_rest`) using `gh api`
- [x] Convert `add_jules_label` to `add_pr_labels`
- [x] Convert `sync_pr_labels_and_assignee` (issue-label fetch, label apply, assignee) to REST
- [x] Convert `create_missing_issue` (link + create + link) to REST
- [x] Convert the Jules bug/enhancement labels to `add_pr_labels`
- [x] Add `review_is_approved()` and wire it into `main()`
- [x] Categorize auto-trace issues (`bug` for Sentinel, else `enhancement`)

## Testing

- [x] `test_ai_reviewer_approval.py`: approval detection (12 cases incl. Jules prefix, blocker, ECC block)
- [x] `test_ai_reviewer_approval.py`: REST helper call shapes (monkeypatched `_gh_api`)
- [x] `test_ai_reviewer_ecc.py` still passes (7 tests)
- [x] `python3 -m py_compile` clean

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] no-blog justification (Rule 76)
- [x] PR links tracking issue #1140 (Rule 11)
