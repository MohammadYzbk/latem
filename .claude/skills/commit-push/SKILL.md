---
name: commit-push
description: Applies the repository git workflow for reviewing the pending diff, committing, pushing, and creating or updating a pull request. Use when preparing a change for commit, push, or pull request creation.
argument-hint: [issue-number]
allowed-tools: Agent Bash(gh *) Bash(git *) Read
---

# Commit Push

1. Read the rules. Read [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) and, for a pull request, the [pull-request template](../../../.github/PULL_REQUEST_TEMPLATE.md).
2. Inspect the changes. Run `git status --short`, `git diff --stat HEAD`, and `git diff HEAD`; read relevant untracked files before including them. Stop if there is neither a pending diff nor an approved unpublished commit.
3. Verify the branch. Run `git branch --show-current`, inspect the upstream and unpublished commits, and stop on detached `HEAD`. Follow the branch format in `CONTRIBUTING.md`; if the branch is `main` or invalid, derive a topic branch from the supplied issue or touched subproject and diff, then confirm before switching. For a supplied issue number, run `gh issue view <issue> --json number,title,labels`.
4. Choose the publication mode: commit only, push, or pull request.
5. Confirm the plan. Before the first mutation, confirm the paths, branch, commit message and grouping, history action, remote or PR base, issue link, and check status. A known failed required check blocks publication.
6. Stage the changes. Stage only reviewed paths and inspect `git diff --cached` for unrelated or sensitive content.
7. Create the commit. Use the repository's format and add the required `Co-Authored-By:` trailer when an AI agent contributed. Create a new commit by default; amend, fixup, rebase, and force updates require explicit approval.
8. Push the branch when requested or needed for a pull request. Run `git push -u origin HEAD` and verify the pushed branch.
9. Publish the pull request when requested. Inspect `gh pr view --json number,url,title`, update an existing pull request or create an assigned draft targeting `main`, and follow the repository's title and body format. Preserve its state and meaningful body content.
10. Report the result. Stop at the requested publication step and report the commit, branch, push, or pull-request URL and missing checks. Do not mark a pull request ready or merge it.
