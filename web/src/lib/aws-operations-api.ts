import { fetchAPI } from './api';
import type { AWSLambdaResource, AWSOperationService, AWSOperationWorkspace, AWSSQSMessage } from './aws-operations-types';

const base = '/aws/operations/workspaces';
export const listAWSOperationsWorkspaces = () => fetchAPI<{ workspaces: AWSOperationWorkspace[] }>(base);
export const getAWSOperationsWorkspace = (id: string) => fetchAPI<AWSOperationWorkspace>(`${base}/${encodeURIComponent(id)}`);
export const listAWSOperationServices = (id: string) => fetchAPI<{ workspace_id: string; services: AWSOperationService[] }>(`${base}/${encodeURIComponent(id)}/services`);
export const listAWSOperationResources = <T = unknown>(id: string, service: string) => fetchAPI<{ workspace_id: string; service: string; resources: T[] }>(`${base}/${encodeURIComponent(id)}/services/${encodeURIComponent(service)}/resources`);
export const invokeAWSLambda = (workspaceId: string, resourceId: string, payload: unknown) => fetchAPI<unknown>(`${base}/${encodeURIComponent(workspaceId)}/services/lambda/resources/${encodeURIComponent(resourceId)}/invoke`, { method: 'POST', body: JSON.stringify(payload) });
export const deleteAWSLambda = (workspaceId: string, resourceId: string) => fetchAPI<{ status: string }>(`${base}/${encodeURIComponent(workspaceId)}/services/lambda/resources/${encodeURIComponent(resourceId)}`, { method: 'DELETE' });
export const updateAWSLambda = (workspaceId: string, resourceId: string, input: Partial<AWSLambdaResource>) => fetchAPI<AWSLambdaResource>(`${base}/${encodeURIComponent(workspaceId)}/services/lambda/resources/${encodeURIComponent(resourceId)}`, { method: 'PUT', body: JSON.stringify(input) });
export const getAWSLambdaLogs = (workspaceId: string, resourceId: string) => fetchAPI<{ logs: unknown[] }>(`${base}/${encodeURIComponent(workspaceId)}/services/lambda/resources/${encodeURIComponent(resourceId)}/logs`);
export const listAWSSQSMessages = (workspaceId: string, resourceId: string, status?: string) => fetchAPI<{ messages: AWSSQSMessage[] }>(`${base}/${encodeURIComponent(workspaceId)}/services/sqs/resources/${encodeURIComponent(resourceId)}/messages${status ? `?status=${encodeURIComponent(status)}` : ''}`);
export const retryAWSSQSMessage = (workspaceId: string, resourceId: string, messageId: string) => fetchAPI<{ status: string }>(`${base}/${encodeURIComponent(workspaceId)}/services/sqs/resources/${encodeURIComponent(resourceId)}/messages/${encodeURIComponent(messageId)}/retry`, { method: 'POST' });
export const deleteAWSSQSMessage = (workspaceId: string, resourceId: string, messageId: string) => fetchAPI<{ status: string }>(`${base}/${encodeURIComponent(workspaceId)}/services/sqs/resources/${encodeURIComponent(resourceId)}/messages/${encodeURIComponent(messageId)}`, { method: 'DELETE' });
export const purgeAWSSQS = (workspaceId: string, resourceId: string, status: string) => fetchAPI<{ status: string; deleted: number }>(`${base}/${encodeURIComponent(workspaceId)}/services/sqs/resources/${encodeURIComponent(resourceId)}/messages?status=${encodeURIComponent(status)}`, { method: 'DELETE' });
