import { QueueInspector } from '../components/QueueInspector';

export function Queues() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Queues</h1>
        <p className="text-muted-foreground">Inspect and manage RabbitMQ queues</p>
      </div>
      <QueueInspector stackId="default" />
    </div>
  );
}
