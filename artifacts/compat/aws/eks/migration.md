# AWS EKS Migration

This is a migration artifact seed for the supported EKS surface to K3s. It documents local generation inputs only; it is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_eks_cluster`: preserve account, region, cluster name, ARN, IAM role ARN, tags, source import ID, and generated artifact checksum.

## Unsupported actions

- EKS console workflows, account billing, quota purchases, and managed cross-region failover are not mapped by this adapter.

## Operator decisions

1. Select the K3s control-plane endpoint and persistent `eks-data` volume.
2. Map EKS cluster role and tag policy to HomePort authorization bindings.
3. Review generated node, add-on, and access-entry changes before applying them.

## Cutover

1. Import `aws_eks_cluster` records with their source import IDs.
2. Apply `artifacts/compat/aws/eks/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-eks.yaml` before routing compatible calls to `/compat/aws/eks`.

## Rollback

1. Stop routing traffic to `/compat/aws/eks`.
2. Restore the prior EKS endpoint and credentials.
3. Retain source import IDs for reconciliation.
