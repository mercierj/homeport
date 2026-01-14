import { FunctionEditor } from '../components/FunctionEditor';
import { toast } from 'sonner';

export function Functions() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Functions</h1>
        <p className="text-muted-foreground">Manage serverless functions with OpenFaaS</p>
      </div>
      <FunctionEditor
        onError={(err) => toast.error(err.message)}
        onSuccess={(msg) => toast.success(msg)}
      />
    </div>
  );
}
