/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TimerCreateRequest } from './TimerCreateRequest';
export type TimerConflictPreviewRequest = {
    proposed: TimerCreateRequest;
    mode?: TimerConflictPreviewRequest.mode;
};
export namespace TimerConflictPreviewRequest {
    export enum mode {
        CONSERVATIVE = 'conservative',
        RECEIVER_AWARE = 'receiverAware',
    }
}

