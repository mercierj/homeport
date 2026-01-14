import { useState, useCallback } from 'react';
import { useDropzone } from 'react-dropzone';
import {
  Package,
  CheckCircle,
  AlertCircle,
  Loader2,
  FileArchive,
  Server,
  Database,
  Key,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import { uploadBundle } from '@/lib/bundle-api';

export function BundleUploadStep() {
  const {
    uploadedBundle,
    bundleManifest,
    secretRefs,
    setUploadedBundle,
    setBundleId,
    setBundleManifest,
    setSecretRefs,
    setError,
  } = useWizardStore();

  const [isUploading, setIsUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const handleUpload = useCallback(async (file: File) => {
    setIsUploading(true);
    setUploadError(null);
    setUploadedBundle(file);

    try {
      const response = await uploadBundle(file);

      if (!response.valid && response.errors?.length) {
        setUploadError(response.errors.join(', '));
        setIsUploading(false);
        return;
      }

      // Update wizard state with bundle info
      setBundleId(response.bundle_id);
      setBundleManifest(response.manifest);
      setSecretRefs(response.secrets ?? []);
      setIsUploading(false);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to upload bundle';
      setUploadError(message);
      setError(message);
      setIsUploading(false);
    }
  }, [setUploadedBundle, setBundleId, setBundleManifest, setSecretRefs, setError]);

  const onDrop = useCallback((acceptedFiles: File[]) => {
    if (acceptedFiles.length > 0) {
      handleUpload(acceptedFiles[0]);
    }
  }, [handleUpload]);

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop,
    accept: {
      'application/x-hprt': ['.hprt'],
      'application/octet-stream': ['.hprt'],
    },
    maxFiles: 1,
    disabled: isUploading,
  });

  const handleReset = () => {
    setUploadedBundle(null);
    setBundleId(null);
    setBundleManifest(null);
    setSecretRefs([]);
    setUploadError(null);
  };

  // Show upload form if no bundle is loaded
  if (!bundleManifest) {
    return (
      <div className="max-w-2xl mx-auto space-y-6">
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-accent/10 mb-4">
            <Package className="w-8 h-8 text-accent" />
          </div>
          <h2 className="text-2xl font-bold">Upload Migration Bundle</h2>
          <p className="text-muted-foreground mt-2">
            Upload a previously exported .hprt bundle to continue the migration process
          </p>
        </div>

        {/* Dropzone */}
        <div
          {...getRootProps()}
          className={cn(
            'border-2 border-dashed rounded-lg p-12 text-center cursor-pointer transition-all',
            isDragActive && 'border-accent bg-accent/5 scale-[1.02]',
            !isDragActive && 'border-muted-foreground/25 hover:border-accent/50',
            isUploading && 'opacity-50 cursor-not-allowed',
            uploadError && 'border-error/50 bg-error/5'
          )}
        >
          <input {...getInputProps()} />

          {isUploading ? (
            <>
              <Loader2 className="mx-auto h-12 w-12 text-accent animate-spin" />
              <p className="mt-4 text-lg font-medium">Uploading bundle...</p>
              <p className="mt-2 text-sm text-muted-foreground">
                Validating manifest and extracting secrets
              </p>
            </>
          ) : uploadError ? (
            <>
              <AlertCircle className="mx-auto h-12 w-12 text-error" />
              <p className="mt-4 text-lg font-medium text-error">Upload Failed</p>
              <p className="mt-2 text-sm text-error/80">{uploadError}</p>
              <p className="mt-4 text-sm text-muted-foreground">
                Click or drop another file to try again
              </p>
            </>
          ) : (
            <>
              <FileArchive className="mx-auto h-12 w-12 text-muted-foreground" />
              <p className="mt-4 text-lg font-medium">
                {isDragActive ? 'Drop bundle here...' : 'Drag & drop your .hprt bundle'}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                Or click to browse for files
              </p>
            </>
          )}
        </div>

        {/* Help text */}
        <div className="bg-muted/30 rounded-lg p-4">
          <h4 className="font-medium text-sm mb-2">What is a .hprt bundle?</h4>
          <p className="text-sm text-muted-foreground">
            A Homeport bundle (.hprt) contains your analyzed infrastructure, generated
            Docker Compose files, migration scripts, and secrets references. It's created
            during the "Export" step when migrating from cloud sources.
          </p>
        </div>
      </div>
    );
  }

  // Show bundle info after successful upload
  return (
    <div className="max-w-3xl mx-auto space-y-6">
      {/* Success header */}
      <div className="text-center mb-8">
        <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-accent/10 mb-4">
          <CheckCircle className="w-8 h-8 text-accent" />
        </div>
        <h2 className="text-2xl font-bold">Bundle Loaded Successfully</h2>
        <p className="text-muted-foreground mt-2">
          Review the bundle contents before proceeding to provide secrets
        </p>
      </div>

      {/* Bundle summary card */}
      <div className="card-resource p-6">
        <div className="flex items-start justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="resource-icon-storage">
              <Package className="w-5 h-5" />
            </div>
            <div>
              <h3 className="font-semibold">{uploadedBundle?.name || 'Migration Bundle'}</h3>
              <p className="text-sm text-muted-foreground">
                Version {bundleManifest.version} • Created {new Date(bundleManifest.created).toLocaleDateString()}
              </p>
            </div>
          </div>
          <button
            onClick={handleReset}
            className={cn(buttonVariants({ variant: 'ghost', size: 'sm' }))}
          >
            Upload Different
          </button>
        </div>

        {/* Source info */}
        <div className="grid grid-cols-2 gap-4 mb-4">
          <div className="bg-muted/30 rounded-lg p-3">
            <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Source</p>
            <p className="font-medium capitalize">{bundleManifest.source.provider}</p>
            <p className="text-sm text-muted-foreground">
              {bundleManifest.source.resource_count} resources
            </p>
          </div>
          <div className="bg-muted/30 rounded-lg p-3">
            <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Target</p>
            <p className="font-medium capitalize">{bundleManifest.target.type}</p>
            <p className="text-sm text-muted-foreground">
              {bundleManifest.target.stack_count} stacks
            </p>
          </div>
        </div>

        {/* Stacks preview */}
        {bundleManifest.stacks && bundleManifest.stacks.length > 0 && (
          <div className="border-t border-border/50 pt-4">
            <h4 className="text-sm font-medium mb-3">Included Stacks</h4>
            <div className="space-y-2">
              {bundleManifest.stacks.map((stack, index) => (
                <div
                  key={index}
                  className="flex items-center justify-between p-2 bg-muted/20 rounded"
                >
                  <div className="flex items-center gap-2">
                    <Server className="w-4 h-4 text-muted-foreground" />
                    <span className="font-medium">{stack.name}</span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span>{stack.services.length} services</span>
                    {stack.data_sync_required && (
                      <span className="badge-warning text-xs">Sync Required</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Secrets requirements */}
      {secretRefs.length > 0 && (
        <div className="card-resource p-6">
          <div className="flex items-center gap-3 mb-4">
            <div className="resource-icon-security">
              <Key className="w-5 h-5" />
            </div>
            <div>
              <h3 className="font-semibold">Required Secrets</h3>
              <p className="text-sm text-muted-foreground">
                {secretRefs.length} secrets need to be provided in the next step
              </p>
            </div>
          </div>

          <div className="space-y-2">
            {secretRefs.map((secret, index) => (
              <div
                key={index}
                className="flex items-center justify-between p-2 bg-muted/20 rounded"
              >
                <div className="flex items-center gap-2">
                  <Key className="w-4 h-4 text-muted-foreground" />
                  <span className="font-mono text-sm">{secret.name}</span>
                  {secret.required && (
                    <span className="text-error text-xs">*</span>
                  )}
                </div>
                {secret.description && (
                  <span className="text-sm text-muted-foreground truncate max-w-[200px]">
                    {secret.description}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Data sync info */}
      {bundleManifest.data_sync && (
        <div className="card-resource p-6">
          <div className="flex items-center gap-3 mb-4">
            <div className="resource-icon-database">
              <Database className="w-5 h-5" />
            </div>
            <div>
              <h3 className="font-semibold">Data Migration Required</h3>
              <p className="text-sm text-muted-foreground">
                Estimated size: {bundleManifest.data_sync.total_estimated_size}
              </p>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            {bundleManifest.data_sync.databases.length > 0 && (
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide mb-2">Databases</p>
                <div className="space-y-1">
                  {bundleManifest.data_sync.databases.map((db, i) => (
                    <div key={i} className="text-sm font-mono bg-muted/20 px-2 py-1 rounded">
                      {db}
                    </div>
                  ))}
                </div>
              </div>
            )}
            {bundleManifest.data_sync.storage.length > 0 && (
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide mb-2">Storage</p>
                <div className="space-y-1">
                  {bundleManifest.data_sync.storage.map((s, i) => (
                    <div key={i} className="text-sm font-mono bg-muted/20 px-2 py-1 rounded">
                      {s}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* No secrets message */}
      {secretRefs.length === 0 && (
        <div className="bg-accent/10 border border-accent/20 rounded-lg p-4">
          <div className="flex items-center gap-3">
            <CheckCircle className="w-5 h-5 text-accent" />
            <div>
              <p className="font-medium">No Secrets Required</p>
              <p className="text-sm text-muted-foreground">
                This bundle doesn't require any additional secrets. You can proceed directly to deployment.
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
