/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { RuleSnapshot } from './RuleSnapshot';
import type { RunConflict } from './RunConflict';
import type { RunDecision } from './RunDecision';
import type { RunError } from './RunError';
import type { RunSummary } from './RunSummary';
export type SeriesRuleRunReport = {
    ruleId?: string;
    runId?: string;
    trigger?: string;
    startedAt?: string;
    finishedAt?: string;
    durationMs?: number;
    windowFrom?: number;
    windowTo?: number;
    status?: SeriesRuleRunReport.status;
    summary?: RunSummary;
    snapshot?: RuleSnapshot;
    decisions?: Array<RunDecision>;
    errors?: Array<RunError>;
    conflicts?: Array<RunConflict>;
};
export namespace SeriesRuleRunReport {
    export enum status {
        SUCCESS = 'success',
        PARTIAL = 'partial',
        FAILED = 'failed',
    }
}

