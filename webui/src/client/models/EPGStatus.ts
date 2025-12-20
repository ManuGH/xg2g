/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type EPGStatus = {
    status?: EPGStatus.status;
    missing_channels?: number;
};
export namespace EPGStatus {
    export enum status {
        OK = 'ok',
        MISSING = 'missing',
    }
}

