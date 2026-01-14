import { BackupManager } from '../components/BackupManager';

export function Backup() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Backup</h1>
        <p className="text-muted-foreground">Create, restore, and manage backups</p>
      </div>
      <BackupManager stackId="default" />
    </div>
  );
}
