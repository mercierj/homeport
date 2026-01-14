import { FileBrowser } from '@/components/FileBrowser';

export function Storage() {
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Storage</h1>
      <FileBrowser />
    </div>
  );
}
