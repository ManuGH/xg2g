/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Timer } from './Timer';
export type TimerConflict = {
    type: TimerConflict.type;
    blockingTimer: Timer;
    overlapSeconds?: number;
    message?: string;
};
export namespace TimerConflict {
    export enum type {
        OVERLAP = 'overlap',
        DUPLICATE = 'duplicate',
        TUNER_LIMIT = 'tuner_limit',
        UNKNOWN = 'unknown',
    }
}

