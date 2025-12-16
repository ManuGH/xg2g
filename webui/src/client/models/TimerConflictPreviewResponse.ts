/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TimerConflict } from './TimerConflict';
export type TimerConflictPreviewResponse = {
    canSchedule: boolean;
    conflicts: Array<TimerConflict>;
    suggestions?: Array<{
        kind?: 'reduce_padding' | 'shift_start' | 'shift_end';
        proposedBegin?: number;
        proposedEnd?: number;
        note?: string;
    }>;
};

