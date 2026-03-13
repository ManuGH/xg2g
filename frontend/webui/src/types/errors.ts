export interface AppError {
  title: string;
  detail?: string;
  status?: number;
  retryable: boolean;
}
