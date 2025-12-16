/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ComponentStatus } from './ComponentStatus';
import type { EPGStatus } from './EPGStatus';
export type SystemHealth = {
    status?: SystemHealth.status;
    receiver?: ComponentStatus;
    epg?: EPGStatus;
    version?: string;
    uptime_seconds?: number;
};
export namespace SystemHealth {
    export enum status {
        OK = 'ok',
        DEGRADED = 'degraded',
        ERROR = 'error',
    }
}

