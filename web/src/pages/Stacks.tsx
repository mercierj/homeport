import { StackManager } from '../components/StackManager';

export function Stacks() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Stacks</h1>
        <p className="text-muted-foreground">Manage multiple deployment stacks</p>
      </div>
      <StackManager />
    </div>
  );
}
