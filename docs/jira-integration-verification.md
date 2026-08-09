# Jira Integration Verification

Jira issue: [SCRUM-1](https://liem-dang.atlassian.net/browse/SCRUM-1)

This document records the successful end-to-end verification of the Jira-to-GitHub workflow.

## Verified steps

- Read the Jira task through the Atlassian Rovo integration.
- Checked for an existing pull request referencing the Jira key.
- Transitioned the Jira task from `To Do` to `In Progress`.
- Created a dedicated Git branch from `main`.
- Created a commit associated with `SCRUM-1`.
- Opened a draft pull request for review.
- Linked the pull request back to Jira.

## Automation invariant

A scheduled run must process at most one eligible Jira task and must not create a second open pull request for the same Jira key.
