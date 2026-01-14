import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  listQueues,
  listMessages,
  getMessage,
  retryMessage,
  deleteMessage,
  purgeQueue,
  type Queue,
  type Message,
  type MessageStatus,
} from '@/lib/queues-api';

import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
  List,
  RefreshCw,
  Trash2,
  RotateCcw,
  ChevronRight,
  ChevronDown,
  Clock,
  CheckCircle,
  XCircle,
  Loader2,
  AlertTriangle,
} from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

const STATUS_ICONS: Record<MessageStatus, React.ReactNode> = {
  pending: <Clock className="h-4 w-4 text-yellow-500" />,
  active: <Loader2 className="h-4 w-4 text-blue-500 animate-spin" />,
  completed: <CheckCircle className="h-4 w-4 text-green-500" />,
  failed: <XCircle className="h-4 w-4 text-error" />,
};

const STATUS_LABELS: Record<MessageStatus, string> = {
  pending: 'Pending',
  active: 'Active',
  completed: 'Completed',
  failed: 'Failed',
};

export function QueueInspector({ stackId = 'default' }: { stackId?: string }) {
  const [selectedQueue, setSelectedQueue] = useState<string | null>(null);
  const [selectedStatus, setSelectedStatus] = useState<MessageStatus>('pending');
  const [expandedMessage, setExpandedMessage] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const queuesQuery = useQuery({
    queryKey: ['queues', stackId],
    queryFn: () => listQueues(stackId),
    refetchInterval: 5000,
  });

  const messagesQuery = useQuery({
    queryKey: ['queue-messages', stackId, selectedQueue, selectedStatus],
    queryFn: () => listMessages(stackId, selectedQueue!, { status: selectedStatus }),
    enabled: !!selectedQueue,
    refetchInterval: 3000,
  });

  const messageQuery = useQuery({
    queryKey: ['queue-message', stackId, selectedQueue, expandedMessage],
    queryFn: () => getMessage(stackId, selectedQueue!, expandedMessage!),
    enabled: !!selectedQueue && !!expandedMessage,
  });

  const retryMutation = useMutation({
    mutationFn: (messageId: string) => retryMessage(stackId, selectedQueue!, messageId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue-messages', stackId, selectedQueue] });
      queryClient.invalidateQueries({ queryKey: ['queues', stackId] });
      toast.success('Message queued for retry');
    },
    onError: (error) => {
      toast.error('Failed to retry message', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (messageId: string) => deleteMessage(stackId, selectedQueue!, messageId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue-messages', stackId, selectedQueue] });
      queryClient.invalidateQueries({ queryKey: ['queues', stackId] });
      setExpandedMessage(null);
      toast.success('Message deleted');
    },
    onError: (error) => {
      toast.error('Failed to delete message', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const purgeMutation = useMutation({
    mutationFn: () => purgeQueue(stackId, selectedQueue!, selectedStatus),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue-messages', stackId, selectedQueue] });
      queryClient.invalidateQueries({ queryKey: ['queues', stackId] });
      toast.success('Queue purged');
    },
    onError: (error) => {
      toast.error('Failed to purge queue', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const getQueueStats = (queue: Queue) => {
    const total = queue.pending_count + queue.active_count + queue.completed_count + queue.failed_count;
    return { total, ...queue };
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Queue Inspector</h2>
        <Button
          variant="outline"
          size="sm"
          onClick={() => queuesQuery.refetch()}
          disabled={queuesQuery.isFetching}
        >
          <RefreshCw className={cn("h-4 w-4 mr-2", queuesQuery.isFetching && "animate-spin")} />
          Refresh
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        {/* Queue List */}
        <div className="border rounded-lg p-4">
          <h3 className="font-medium mb-2">Queues</h3>

          {queuesQuery.isLoading ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : queuesQuery.error ? (
            <p className="text-sm text-destructive">Failed to load queues</p>
          ) : (
            <ul className="space-y-1">
              {queuesQuery.data?.queues.map((queue) => {
                const stats = getQueueStats(queue);
                return (
                  <li key={queue.name}>
                    <button
                      onClick={() => {
                        setSelectedQueue(queue.name);
                        setExpandedMessage(null);
                      }}
                      className={cn(
                        "w-full text-left px-2 py-2 rounded text-sm",
                        selectedQueue === queue.name
                          ? "bg-primary text-primary-foreground"
                          : "hover:bg-muted"
                      )}
                    >
                      <div className="flex items-center justify-between">
                        <span className="truncate">{queue.name}</span>
                        <span className="text-xs opacity-70">{stats.total}</span>
                      </div>
                      {queue.failed_count > 0 && (
                        <div className="flex items-center gap-1 mt-1 text-xs text-error">
                          <AlertTriangle className="h-3 w-3" />
                          {queue.failed_count} failed
                        </div>
                      )}
                    </button>
                  </li>
                );
              })}
              {queuesQuery.data?.queues.length === 0 && (
                <li className="text-sm text-muted-foreground px-2">No queues found</li>
              )}
            </ul>
          )}
        </div>

        {/* Messages Panel */}
        <div className="md:col-span-3 border rounded-lg p-4">
          {selectedQueue ? (
            <>
              {/* Status Tabs */}
              <div className="flex items-center justify-between mb-4">
                <div className="flex gap-1">
                  {(['pending', 'active', 'completed', 'failed'] as MessageStatus[]).map((status) => (
                    <Button
                      key={status}
                      variant={selectedStatus === status ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => {
                        setSelectedStatus(status);
                        setExpandedMessage(null);
                      }}
                    >
                      {STATUS_ICONS[status]}
                      <span className="ml-1">{STATUS_LABELS[status]}</span>
                    </Button>
                  ))}
                </div>
                <Button
                  variant="error"
                  size="sm"
                  onClick={() => purgeMutation.mutate()}
                  disabled={purgeMutation.isPending}
                >
                  <Trash2 className="h-4 w-4 mr-1" />
                  Purge Queue
                </Button>
              </div>

              {/* Messages List */}
              {messagesQuery.isLoading ? (
                <div className="space-y-2">
                  {[1, 2, 3, 4, 5].map((i) => (
                    <Skeleton key={i} className="h-12 w-full" />
                  ))}
                </div>
              ) : messagesQuery.error ? (
                <p className="text-destructive">Failed to load messages</p>
              ) : (
                <ul className="space-y-2">
                  {messagesQuery.data?.messages.length === 0 && (
                    <li className="text-muted-foreground py-4 text-center">
                      No {selectedStatus} messages
                    </li>
                  )}
                  {messagesQuery.data?.messages.map((msg: Message) => (
                    <li key={msg.id} className="border rounded">
                      <button
                        onClick={() => setExpandedMessage(expandedMessage === msg.id ? null : msg.id)}
                        className="w-full flex items-center justify-between p-3 hover:bg-muted/50"
                      >
                        <div className="flex items-center gap-2">
                          {expandedMessage === msg.id ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                          {STATUS_ICONS[msg.status]}
                          <span className="font-mono text-sm truncate max-w-[200px]">
                            {msg.id}
                          </span>
                        </div>
                        <div className="flex items-center gap-4 text-sm text-muted-foreground">
                          <span>Attempts: {msg.attempts}</span>
                          <span>{new Date(msg.created_at).toLocaleString()}</span>
                        </div>
                      </button>

                      {expandedMessage === msg.id && (
                        <div className="p-3 border-t bg-muted/30">
                          {messageQuery.isLoading ? (
                            <Skeleton className="h-24 w-full" />
                          ) : messageQuery.error ? (
                            <p className="text-destructive text-sm">Failed to load message details</p>
                          ) : (
                            <>
                              <div className="mb-3">
                                <h4 className="text-sm font-medium mb-1">Data</h4>
                                <pre className="text-xs bg-background p-2 rounded overflow-auto max-h-48">
                                  {JSON.stringify(messageQuery.data?.data, null, 2)}
                                </pre>
                              </div>

                              {msg.error && (
                                <div className="mb-3">
                                  <h4 className="text-sm font-medium mb-1 text-destructive">Error</h4>
                                  <pre className="text-xs bg-destructive/10 text-destructive p-2 rounded overflow-auto">
                                    {msg.error}
                                  </pre>
                                </div>
                              )}

                              <div className="flex gap-2">
                                {msg.status === 'failed' && (
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => retryMutation.mutate(msg.id)}
                                    disabled={retryMutation.isPending}
                                  >
                                    <RotateCcw className="h-4 w-4 mr-1" />
                                    Retry
                                  </Button>
                                )}
                                <Button
                                  size="sm"
                                  variant="error"
                                  onClick={() => deleteMutation.mutate(msg.id)}
                                  disabled={deleteMutation.isPending}
                                >
                                  <Trash2 className="h-4 w-4 mr-1" />
                                  Delete
                                </Button>
                              </div>
                            </>
                          )}
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <List className="h-8 w-8 mr-2" />
              <span>Select a queue to inspect messages</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
