/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TimerConflict } from './TimerConflict';
export type ProblemDetails = {
    type: string;
    title: string;
    status: number;
    detail?: string;
    instance?: string;
    fields?: Record<string, any>;
    conflicts?: Array<TimerConflict>;
};

