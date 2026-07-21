# AWS CodeBuild Migration

This is a local adapter seed for GitLab CI. It does not prove a GitLab deployment, build execution, persistence, cutover, or rollback.

## Source import IDs

- `aws_codebuild_project`: preserve project name, source, buildspec, environment image, artifacts, and service role as migration input.

## Unsupported actions

- Build execution, webhooks, reports, batch builds, and GitLab delivery are outside this adapter.

## Operator decisions

1. Review the generated GitLab CI mapping from the existing CodeBuild mapper.
2. Keep AWS authoritative until external build validation is performed.
