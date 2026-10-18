export interface WorkflowMeta {
  description: string;
  model: string;
  provider: string;
  version: string;
  temperature: number;
  maxTokens: number;
  timeout: string;
  retries: number;
  endpoint: string;
}
