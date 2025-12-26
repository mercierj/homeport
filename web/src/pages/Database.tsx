import { useState } from 'react';
import { useCredentialsStore } from '@/stores/credentials';
import { DatabaseCredentialsModal } from '@/components/DatabaseCredentialsModal';
import { QueryEditor } from '@/components/QueryEditor';
import { TableBrowser } from '@/components/TableBrowser';
import { Button } from '@/components/ui/button';
import { Settings } from 'lucide-react';

export function Database() {
  const [showCredentials, setShowCredentials] = useState(false);
  const hasCredentials = useCredentialsStore((s) => !!s.database);

  if (!hasCredentials) {
    return (
      <div className="space-y-6">
        <h1 className="text-3xl font-bold">Database</h1>
        <div className="text-center py-12 border rounded-lg">
          <p className="text-muted-foreground mb-4">Configure database credentials to connect</p>
          <Button onClick={() => setShowCredentials(true)}>
            <Settings className="h-4 w-4 mr-2" />
            Configure Credentials
          </Button>
        </div>
        {showCredentials && (
          <DatabaseCredentialsModal onClose={() => setShowCredentials(false)} />
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Database</h1>
        <Button variant="outline" size="sm" onClick={() => setShowCredentials(true)}>
          <Settings className="h-4 w-4 mr-2" />
          Credentials
        </Button>
      </div>

      <QueryEditor />
      <TableBrowser />

      {showCredentials && (
        <DatabaseCredentialsModal onClose={() => setShowCredentials(false)} />
      )}
    </div>
  );
}
