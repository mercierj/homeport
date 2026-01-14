import { fetchAPI } from './api';

export type MessageStatus = 'pending' | 'active' | 'completed' | 'failed';

export interface Queue {
  name: string;
  pending_count: number;
  active_count: number;
  completed_count: number;
  failed_count: number;
  total_count: number;
  created_at?: string;
  updated_at?: string;
}

export interface Message {
  id: string;
  queue_name: string;
  status: MessageStatus;
  data: Record<string, unknown>;
  attempts: number;
  max_attempts: number;
  error?: string;
  created_at: string;
  processed_at?: string;
  completed_at?: string;
  failed_at?: string;
}

export interface QueuesResponse {
  queues: Queue[];
  count: number;
}

export interface MessagesResponse {
  messages: Message[];
  count: number;
  limit: number;
  offset: number;
  status: MessageStatus;
}

export async function listQueues(stackId: string = 'default'): Promise<QueuesResponse> {
  return fetchAPI<QueuesResponse>(`/stacks/${stackId}/queues`);
}

export async function getQueue(stackId: string, queueName: string): Promise<Queue> {
  return fetchAPI<Queue>(`/stacks/${stackId}/queues/${encodeURIComponent(queueName)}`);
}

export async function listMessages(
  stackId: string,
  queueName: string,
  options: {
    status?: MessageStatus;
    limit?: number;
    offset?: number;
  } = {}
): Promise<MessagesResponse> {
  const params = new URLSearchParams();
  if (options.status) params.set('status', options.status);
  if (options.limit) params.set('limit', options.limit.toString());
  if (options.offset) params.set('offset', options.offset.toString());

  const query = params.toString();
  const url = `/stacks/${stackId}/queues/${encodeURIComponent(queueName)}/messages${query ? `?${query}` : ''}`;
  return fetchAPI<MessagesResponse>(url);
}

export async function getMessage(
  stackId: string,
  queueName: string,
  messageId: string
): Promise<Message> {
  return fetchAPI<Message>(
    `/stacks/${stackId}/queues/${encodeURIComponent(queueName)}/messages/${encodeURIComponent(messageId)}`
  );
}

export async function deleteMessage(
  stackId: string,
  queueName: string,
  messageId: string
): Promise<{ status: string; messageID: string }> {
  return fetchAPI<{ status: string; messageID: string }>(
    `/stacks/${stackId}/queues/${encodeURIComponent(queueName)}/messages/${encodeURIComponent(messageId)}`,
    { method: 'DELETE' }
  );
}

export async function retryMessage(
  stackId: string,
  queueName: string,
  messageId: string
): Promise<{ status: string; messageID: string }> {
  return fetchAPI<{ status: string; messageID: string }>(
    `/stacks/${stackId}/queues/${encodeURIComponent(queueName)}/messages/${encodeURIComponent(messageId)}/retry`,
    { method: 'POST' }
  );
}

export async function purgeQueue(
  stackId: string,
  queueName: string,
  status: MessageStatus
): Promise<{ status: string; deleted: number }> {
  return fetchAPI<{ status: string; deleted: number }>(
    `/stacks/${stackId}/queues/${encodeURIComponent(queueName)}/messages?status=${status}`,
    { method: 'DELETE' }
  );
}
