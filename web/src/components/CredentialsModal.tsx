import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { useCredentialsStore } from '@/stores/credentials';
import { X } from 'lucide-react';

interface CredentialsModalProps {
  onClose: () => void;
}

export function CredentialsModal({ onClose }: CredentialsModalProps) {
  const { storage, setStorageCredentials } = useCredentialsStore();
  const [endpoint, setEndpoint] = useState(storage?.endpoint || 'localhost:9000');
  const [accessKey, setAccessKey] = useState(storage?.accessKey || '');
  const [secretKey, setSecretKey] = useState(storage?.secretKey || '');

  const handleSave = () => {
    setStorageCredentials({ endpoint, accessKey, secretKey });
    onClose();
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-md p-6">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold">Storage Credentials</h3>
          <Button variant="ghost" size="sm" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        <p className="text-sm text-muted-foreground mb-4">
          Credentials are stored in browser memory only and never sent to the server for storage.
        </p>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">Endpoint</label>
            <input
              type="text"
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
              className="w-full px-3 py-2 border rounded-md bg-background"
              placeholder="localhost:9000"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Access Key</label>
            <input
              type="text"
              value={accessKey}
              onChange={(e) => setAccessKey(e.target.value)}
              className="w-full px-3 py-2 border rounded-md bg-background"
              placeholder="minioadmin"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Secret Key</label>
            <input
              type="password"
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value)}
              className="w-full px-3 py-2 border rounded-md bg-background"
              placeholder="••••••••"
            />
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={handleSave}>Save</Button>
        </div>
      </div>
    </div>
  );
}
