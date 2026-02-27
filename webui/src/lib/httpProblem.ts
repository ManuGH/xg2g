import type { ProblemDetails } from '../client-ts/types.gen';

type JsonRecord = Record<string, unknown>;

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function headerValue(headers: Headers, key: string): string | null {
  return headers.get(key) ?? headers.get(key.toLowerCase());
}

function toOptionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

async function safeReadJsonBody(res: Response): Promise<JsonRecord | null> {
  const src = typeof res.clone === 'function' ? res.clone() : res;
  try {
    const parsed: unknown = await src.json();
    if (!isRecord(parsed)) return null;
    return parsed;
  } catch {
    return null;
  }
}

function normalizeProblemDetails(body: JsonRecord, res: Response): ProblemDetails | null {
  const hasProblemSignals =
    typeof body.type === 'string' ||
    typeof body.title === 'string' ||
    typeof body.code === 'string';
  if (!hasProblemSignals) return null;

  const status = typeof body.status === 'number' ? body.status : res.status;
  if (!Number.isFinite(status)) return null;

  const type = toOptionalString(body.type) ?? 'about:blank';
  const title = toOptionalString(body.title) ?? `HTTP ${res.status}`;
  const requestId = toOptionalString(body.requestId) ?? headerValue(res.headers, 'X-Request-ID') ?? 'unknown';

  const out: ProblemDetails = {
    type,
    title,
    status,
    requestId
  };

  const code = toOptionalString(body.code);
  if (code) out.code = code;
  const detail = toOptionalString(body.detail);
  if (detail) out.detail = detail;
  const instance = toOptionalString(body.instance);
  if (instance) out.instance = instance;
  if (isRecord(body.fields)) out.fields = body.fields;
  if (Array.isArray(body.conflicts)) out.conflicts = body.conflicts as any;

  return out;
}

export type ParsedProblemResponse = {
  body: JsonRecord | null;
  problem: ProblemDetails | null;
};

export async function parseProblemResponse(res: Response): Promise<ParsedProblemResponse> {
  const body = await safeReadJsonBody(res);
  const problem = body ? normalizeProblemDetails(body, res) : null;
  return { body, problem };
}

export async function parseProblem(res: Response): Promise<ProblemDetails | null> {
  const parsed = await parseProblemResponse(res);
  return parsed.problem;
}

export function formatProblemMessage(problem: ProblemDetails | null, fallback: string): string {
  if (!problem) return fallback;
  const segments: string[] = [];
  if (problem.code) segments.push(problem.code);
  if (problem.title) segments.push(problem.title);
  if (problem.type) segments.push(problem.type);
  if (problem.detail) segments.push(problem.detail);
  return segments.length > 0 ? segments.join(' | ') : fallback;
}

type HttpProblemErrorOptions = {
  status: number;
  problem: ProblemDetails | null;
  requestId?: string;
};

export class HttpProblemError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails | null;
  readonly requestId?: string;

  constructor(message: string, options: HttpProblemErrorOptions) {
    super(message);
    this.name = 'HttpProblemError';
    this.status = options.status;
    this.problem = options.problem;
    this.requestId = options.requestId;
    Object.setPrototypeOf(this, HttpProblemError.prototype);
  }
}

export async function assertOkOrProblem(res: Response, fallback: string): Promise<void> {
  if (res.ok) return;
  const { problem } = await parseProblemResponse(res);
  const requestId = problem?.requestId ?? headerValue(res.headers, 'X-Request-ID') ?? undefined;
  const message = formatProblemMessage(problem, fallback);
  throw new HttpProblemError(message, {
    status: res.status,
    problem,
    requestId
  });
}
