import { useEffect, useRef } from 'react';
import type { LogEvent } from '@/lib/deploy-api';

interface DeploymentLogsProps {
  logs: LogEvent[];
}

export function DeploymentLogs({ logs }: DeploymentLogsProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'error':
        return 'text-red-400';
      case 'warn':
        return 'text-amber-400';
      default:
        return 'text-gray-300';
    }
  };

  const formatTimestamp = (timestamp: string) => {
    try {
      return new Date(timestamp).toLocaleTimeString();
    } catch {
      return timestamp;
    }
  };

  return (
    <div
      ref={containerRef}
      className="terminal-body h-48 overflow-y-auto rounded-lg p-3 font-mono text-xs"
    >
      {logs.length === 0 ? (
        <div className="text-gray-500">Waiting for logs...</div>
      ) : (
        logs.map((log, index) => (
          <div key={index} className="flex gap-2">
            <span className="text-gray-500 flex-shrink-0">
              {formatTimestamp(log.timestamp)}
            </span>
            <span className={getLevelColor(log.level)}>{log.message}</span>
          </div>
        ))
      )}
    </div>
  );
}
