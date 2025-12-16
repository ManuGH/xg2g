/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { RunSummary } from './RunSummary';
export type SeriesRule = {
    readonly id?: string;
    enabled?: boolean;
    /**
     * Search term or regex for event title
     */
    keyword?: string;
    /**
     * Optional service reference to restrict rule
     */
    channel_ref?: string;
    /**
     * Days of week (0=Sunday)
     */
    days?: Array<number>;
    /**
     * Time window HHMM-HHMM
     */
    start_window?: string;
    priority?: number;
    lastRunAt?: string;
    lastRunStatus?: string;
    lastRunSummary?: RunSummary;
};

