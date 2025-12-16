/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type TimerCreateRequest = {
    serviceRef: string;
    begin: number;
    end: number;
    name: string;
    description?: string;
    enabled?: boolean;
    justPlay?: boolean;
    afterEvent?: TimerCreateRequest.afterEvent;
    paddingBeforeSec?: number;
    paddingAfterSec?: number;
    idempotencyKey?: string;
};
export namespace TimerCreateRequest {
    export enum afterEvent {
        DEFAULT = 'default',
        STANDBY = 'standby',
        DEEPSTANDBY = 'deepstandby',
        NOTHING = 'nothing',
    }
}

