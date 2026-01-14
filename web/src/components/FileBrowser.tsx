import { useState, useRef, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { listBuckets, listObjects, uploadFile, deleteObject, createBucket } from '@/lib/storage-api';

import { useCredentialsStore } from '@/stores/credentials';
import { CredentialsModal } from './CredentialsModal';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { Folder, File, Upload, Trash2, ChevronRight, Settings, Plus, Loader2 } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

const MAX_FILE_SIZE = 100 * 1024 * 1024; // 100MB

// S3-compatible bucket naming rules
const BUCKET_NAME_REGEX = /^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/;
const BUCKET_CONSECUTIVE_HYPHENS = /--/;

function validateBucketName(name: string): string | null {
  if (name.length < 3) return 'Bucket name must be at least 3 characters';
  if (name.length > 63) return 'Bucket name must be at most 63 characters';
  if (!BUCKET_NAME_REGEX.test(name)) {
    return 'Bucket name must contain only lowercase letters, numbers, and hyphens, and must start/end with a letter or number';
  }
  if (BUCKET_CONSECUTIVE_HYPHENS.test(name)) {
    return 'Bucket name cannot contain consecutive hyphens';
  }
  return null;
}

export function FileBrowser({ stackId = 'default' }: { stackId?: string }) {
  const [showCredentials, setShowCredentials] = useState(false);
  const [selectedBucket, setSelectedBucket] = useState<string | null>(null);
  const [currentPath, setCurrentPath] = useState('');
  const [showNewBucket, setShowNewBucket] = useState(false);
  const [newBucketName, setNewBucketName] = useState('');
  const [bucketNameError, setBucketNameError] = useState<string | null>(null);
  const [deletingKey, setDeletingKey] = useState<string | null>(null);
  const uploadDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const queryClient = useQueryClient();
  const hasCredentials = useCredentialsStore((s) => !!s.storage);

  const bucketsQuery = useQuery({
    queryKey: ['buckets', stackId],
    queryFn: () => listBuckets(stackId),
    enabled: hasCredentials,
  });

  const objectsQuery = useQuery({
    queryKey: ['objects', stackId, selectedBucket, currentPath],
    queryFn: () => listObjects(stackId, selectedBucket!, currentPath),
    enabled: hasCredentials && !!selectedBucket,
  });

  const uploadMutation = useMutation({
    mutationFn: (file: File) => uploadFile(stackId, selectedBucket!, file, currentPath + file.name),
    onSuccess: (_, file) => {
      queryClient.invalidateQueries({ queryKey: ['objects', stackId, selectedBucket, currentPath], exact: true });
      toast.success(`Uploaded ${file.name}`);
    },
    onError: (error, file) => {
      toast.error(`Failed to upload ${file.name}`, {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (key: string) => deleteObject(stackId, selectedBucket!, key),
    onSuccess: (_, key) => {
      queryClient.invalidateQueries({ queryKey: ['objects', stackId, selectedBucket, currentPath], exact: true });
      setDeletingKey(null);
      toast.success(`Deleted ${key.split('/').pop()}`);
    },
    onError: (error, key) => {
      setDeletingKey(null);
      toast.error(`Failed to delete ${key.split('/').pop()}`, {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const createBucketMutation = useMutation({
    mutationFn: (name: string) => createBucket(stackId, name),
    onSuccess: (_, name) => {
      queryClient.invalidateQueries({ queryKey: ['buckets', stackId], exact: true });
      setShowNewBucket(false);
      setNewBucketName('');
      setBucketNameError(null);
      toast.success(`Created bucket "${name}"`);
    },
    onError: (error) => {
      toast.error('Failed to create bucket', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const handleFileUpload = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) {
      return;
    }

    // Clear any pending debounce
    if (uploadDebounceRef.current) {
      clearTimeout(uploadDebounceRef.current);
    }

    // Debounce uploads to prevent rapid successive triggers
    uploadDebounceRef.current = setTimeout(() => {
      Array.from(files).forEach((file) => {
        if (file.size > MAX_FILE_SIZE) {
          toast.error(`File "${file.name}" is too large`, {
            description: `Maximum file size is ${formatBytes(MAX_FILE_SIZE)}`,
          });
          return;
        }
        uploadMutation.mutate(file);
      });
    }, 300);

    e.target.value = '';
  }, [uploadMutation]);

  const navigateTo = (path: string) => {
    setCurrentPath(path);
  };

  const breadcrumbs = currentPath.split('/').filter(Boolean);

  if (!hasCredentials) {
    return (
      <div className="text-center py-12 border rounded-lg">
        <p className="text-muted-foreground mb-4">Configure storage credentials to browse files</p>
        <Button onClick={() => setShowCredentials(true)}>
          <Settings className="h-4 w-4 mr-2" />
          Configure Credentials
        </Button>
        {showCredentials && <CredentialsModal onClose={() => setShowCredentials(false)} />}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Storage Browser</h2>
        <Button variant="outline" size="sm" onClick={() => setShowCredentials(true)}>
          <Settings className="h-4 w-4 mr-2" />
          Credentials
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        {/* Bucket List */}
        <div className="border rounded-lg p-4">
          <div className="flex items-center justify-between mb-2">
            <h3 className="font-medium">Buckets</h3>
            <Button variant="ghost" size="sm" onClick={() => setShowNewBucket(true)} aria-label="Create new bucket">
              <Plus className="h-4 w-4" />
            </Button>
          </div>

          {showNewBucket && (
            <div className="mb-2 space-y-1">
              <div className="flex gap-1">
                <input
                  type="text"
                  value={newBucketName}
                  onChange={(e) => {
                    const name = e.target.value.toLowerCase();
                    setNewBucketName(name);
                    setBucketNameError(name ? validateBucketName(name) : null);
                  }}
                  placeholder="bucket-name"
                  className={cn(
                    "flex-1 px-2 py-1 text-sm border rounded bg-background",
                    bucketNameError && "border-destructive"
                  )}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && newBucketName && !bucketNameError) {
                      createBucketMutation.mutate(newBucketName);
                    }
                    if (e.key === 'Escape') {
                      setShowNewBucket(false);
                      setNewBucketName('');
                      setBucketNameError(null);
                    }
                  }}
                />
                <Button
                  size="sm"
                  onClick={() => {
                    const error = validateBucketName(newBucketName);
                    if (error) {
                      setBucketNameError(error);
                      return;
                    }
                    createBucketMutation.mutate(newBucketName);
                  }}
                  disabled={!newBucketName || !!bucketNameError || createBucketMutation.isPending}
                >
                  {createBucketMutation.isPending ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    'Add'
                  )}
                </Button>
              </div>
              {bucketNameError && (
                <p className="text-xs text-destructive">{bucketNameError}</p>
              )}
            </div>
          )}

          {bucketsQuery.isLoading ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : bucketsQuery.error ? (
            <p className="text-sm text-destructive">Failed to load buckets</p>
          ) : (
            <ul className="space-y-1">
              {bucketsQuery.data?.buckets.map((bucket) => (
                <li key={bucket.name}>
                  <button
                    onClick={() => { setSelectedBucket(bucket.name); setCurrentPath(''); }}
                    className={cn(
                      "w-full text-left px-2 py-1 rounded text-sm",
                      selectedBucket === bucket.name ? "bg-primary text-primary-foreground" : "hover:bg-muted"
                    )}
                  >
                    {bucket.name}
                  </button>
                </li>
              ))}
              {bucketsQuery.data?.buckets.length === 0 && (
                <li className="text-sm text-muted-foreground px-2">No buckets</li>
              )}
            </ul>
          )}
        </div>

        {/* File Browser */}
        <div className="md:col-span-3 border rounded-lg p-4">
          {selectedBucket ? (
            <>
              {/* Breadcrumbs */}
              <div className="flex items-center gap-1 mb-4 text-sm">
                <button onClick={() => navigateTo('')} className="hover:underline font-medium">
                  {selectedBucket}
                </button>
                {breadcrumbs.map((crumb, i) => (
                  <span key={i} className="flex items-center">
                    <ChevronRight className="h-4 w-4 text-muted-foreground" />
                    <button
                      onClick={() => navigateTo(breadcrumbs.slice(0, i + 1).join('/') + '/')}
                      className="hover:underline"
                    >
                      {crumb}
                    </button>
                  </span>
                ))}
              </div>

              {/* Upload */}
              <div className="mb-4 flex items-center gap-2">
                <label className="cursor-pointer inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors border border-input bg-background hover:bg-accent hover:text-accent-foreground h-9 px-3">
                  <input
                    type="file"
                    multiple
                    onChange={handleFileUpload}
                    className="hidden"
                  />
                  <Upload className="h-4 w-4 mr-2" />
                  Upload Files
                </label>
                {uploadMutation.isPending && (
                  <span className="text-sm text-muted-foreground">Uploading...</span>
                )}
              </div>

              {/* Objects List */}
              {objectsQuery.isLoading ? (
                <div className="space-y-2">
                  {[1, 2, 3, 4, 5].map((i) => (
                    <div key={i} className="flex items-center justify-between p-2">
                      <div className="flex items-center gap-2">
                        <Skeleton className="h-4 w-4" />
                        <Skeleton className="h-4 w-40" />
                      </div>
                      <div className="flex items-center gap-2">
                        <Skeleton className="h-4 w-16" />
                        <Skeleton className="h-8 w-8" />
                      </div>
                    </div>
                  ))}
                </div>
              ) : objectsQuery.error ? (
                <p className="text-destructive">Failed to load objects</p>
              ) : (
                <ul className="space-y-1">
                  {objectsQuery.data?.objects.length === 0 && (
                    <li className="text-muted-foreground py-4 text-center">
                      This folder is empty
                    </li>
                  )}
                  {objectsQuery.data?.objects.map((obj) => (
                    <li
                      key={obj.key}
                      className="flex items-center justify-between p-2 rounded hover:bg-muted"
                    >
                      <div className="flex items-center gap-2">
                        {obj.is_dir ? (
                          <Folder className="h-4 w-4 text-blue-500" />
                        ) : (
                          <File className="h-4 w-4 text-gray-500" />
                        )}
                        {obj.is_dir ? (
                          <button
                            onClick={() => navigateTo(obj.key)}
                            className="hover:underline"
                          >
                            {obj.key.replace(currentPath, '')}
                          </button>
                        ) : (
                          <span>{obj.key.replace(currentPath, '')}</span>
                        )}
                      </div>
                      {!obj.is_dir && (
                        <div className="flex items-center gap-2">
                          <span className="text-sm text-muted-foreground">
                            {formatBytes(obj.size)}
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setDeletingKey(obj.key);
                              deleteMutation.mutate(obj.key);
                            }}
                            disabled={deleteMutation.isPending}
                            aria-label={`Delete ${obj.key.replace(currentPath, '')}`}
                          >
                            {deletingKey === obj.key ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : (
            <p className="text-muted-foreground text-center py-8">Select a bucket to browse files</p>
          )}
        </div>
      </div>

      {showCredentials && <CredentialsModal onClose={() => setShowCredentials(false)} />}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}
