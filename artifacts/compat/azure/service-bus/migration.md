# Azure Service Bus Migration

This is a migration artifact seed for moving the supported Service Bus queue surface to RabbitMQ with AMQP compatibility. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `azurerm_servicebus_namespace`: preserve the Azure subscription, resource group, namespace name, tags, location, and source import id.
- `azurerm_servicebus_queue`: preserve the namespace relationship, queue name, ARM id, tags, location, and source import id.

## Unsupported actions

- Console-only Service Bus workflows.
- Account billing and quota purchase flows.
- Managed cross-region failover controls.
- Service Bus resources outside `Microsoft.ServiceBus/namespaces/queues/read`, `Microsoft.ServiceBus/namespaces/queues/write`, and `Microsoft.ServiceBus/namespaces/queues/delete`.

## Operator decisions

- Confirm the target RabbitMQ with AMQP compatibility cluster and `/compat/azure/service-bus` adapter route.
- Confirm namespace and queue import ids before applying changes.
- Confirm unsupported actions are accepted as exclusions for this compatibility slice.
- Confirm backup reference storage before any production cutover.

## Cutover

1. Import `azurerm_servicebus_namespace` and `azurerm_servicebus_queue` records with source import ids.
2. Apply the backend artifact in `artifacts/compat/azure/service-bus/backend.yaml`.
3. Apply the adapter mapping in `artifacts/compat/azure/service-bus/adapter.yaml`.
4. Run the Service Bus compatibility adapter contract check from `test/conformance/services/azure-service-bus.yaml`.
5. Route queue read/write/delete traffic to `/compat/azure/service-bus` only after the operator accepts the supported surface and unsupported actions.

## Rollback

1. Stop routing new Service Bus queue traffic to `/compat/azure/service-bus`.
2. Restore the previous Azure Service Bus endpoint and credentials.
3. Keep imported `azurerm_servicebus_namespace` and `azurerm_servicebus_queue` source ids for audit and reconciliation.
4. Restore RabbitMQ queue state from the latest validated backup reference when a retry cutover is needed.
