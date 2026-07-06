# Azure Azure DNS Compatibility Plan

## Goal

Expose the smallest Azure Azure DNS-compatible surface needed to migrate the ledger resources to `CoreDNS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Network/dnsZones/read, Microsoft.Network/dnsZones/write, Microsoft.Network/dnsZones/delete.
- Actions explicitly not supported first: Azure DNS console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.Network/dnsZones/read` and its paired read/list calls.
- Ledger resource types: `azurerm_dns_zone`.
- Provider errors: map Azure DNS authorization failures to Azure access-denied codes, missing `azurerm_dns_zone` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-dns` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_dns_zone`.

## Backend

- Backend: CoreDNS.
- Storage and metadata: Azure DNS state lives in `CoreDNS`; HomePort stores provider identifiers for `azurerm_dns_zone`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `CoreDNS` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-dns`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Network/dnsZones/read, Microsoft.Network/dnsZones/write, Microsoft.Network/dnsZones/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/dnsZones/{name}.
- Context: evaluate Azure DNS calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/dnsZones/{name}`, source IP, request id, user agent, tags/labels on `azurerm_dns_zone`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure DNS actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/dnsZones/{name}` prefix checks, tag/label equality on `azurerm_dns_zone`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-dns` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure DNS provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_dns_zone` records and `CoreDNS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure DNS provider ids, `azurerm_dns_zone` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-dns` backend auth, missing `azurerm_dns_zone`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-dns/backend.yaml` for `CoreDNS` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-dns/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-dns/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-dns.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises DnsZonesGet -> DnsZonesCreateOrUpdate -> DnsZonesList -> DnsZonesDelete against `/compat/azure/azure-dns` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_dns_zone` from `azure/azure-dns`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-dns.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-dns.yaml`, then promote only when that manifest passes in CI.
