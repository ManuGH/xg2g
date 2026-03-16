export type AppErrorSeverity = 'info' | 'warning' | 'error' | 'critical';

export interface AppError {
  title: string;
  detail?: string;
  status?: number;
  retryable: boolean;
  code?: string;
  requestId?: string;
  severity?: AppErrorSeverity;
  operatorHint?: string;
  runbookUrl?: string;
}
