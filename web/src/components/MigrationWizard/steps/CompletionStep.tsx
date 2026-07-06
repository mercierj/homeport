import { CheckCircle2, Home, RotateCcw } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { buttonVariants } from '@/lib/button-variants';
import { cn } from '@/lib/utils';
import { useWizardStore } from '@/stores/wizard';

export function CompletionStep() {
  const navigate = useNavigate();
  const { reset } = useWizardStore();

  const startNew = () => {
    reset();
    navigate('/migrate');
  };

  return (
    <div className="mx-auto max-w-3xl py-10 text-center space-y-6">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
        <CheckCircle2 className="h-8 w-8 text-accent" />
      </div>
      <div>
        <h2 className="text-2xl font-bold">Migration Complete</h2>
        <p className="mt-2 text-muted-foreground">
          The migration journey has completed. Use the operational pages for day-two checks, logs, metrics, and stack management.
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-3">
        <button onClick={() => navigate('/')} className={cn(buttonVariants({ variant: 'primary' }), 'gap-2')}>
          <Home className="h-4 w-4" />
          Dashboard
        </button>
        <button onClick={startNew} className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}>
          <RotateCcw className="h-4 w-4" />
          New Migration
        </button>
      </div>
    </div>
  );
}
