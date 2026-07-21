# AWS ACM to Traefik ACME

1. Export each `aws_acm_certificate` ARN, domain, tags, and source import id.
2. Apply `backend.yaml`, then import the certificate metadata into Traefik ACME.
3. Run `test/conformance/services/aws-acm.yaml` before routing ACM clients to `/compat/aws/acm`.
4. Roll back by restoring the AWS endpoint; retain source import ids and the latest validated Traefik ACME backup for reconciliation.
