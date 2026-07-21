# AWS Step Functions Migration

This is a local adapter seed for Temporal. It does not prove a Temporal deployment, execution migration, cutover, or rollback.

## Source import IDs

- `aws_sfn_state_machine`: preserve the state-machine ARN, name, definition, role ARN, and source import ID.

## Unsupported actions

- Workflow execution, activity workers, versions, aliases, and Temporal delivery are outside this adapter.

## Operator decisions

1. Review the generated Temporal mapping from the existing Step Functions mapper.
2. Keep AWS authoritative until workflow validation is performed externally.
