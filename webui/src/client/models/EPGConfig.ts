/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type EPGConfig = {
    enabled?: boolean;
    days?: number;
    source?: EPGConfig.source;
};
export namespace EPGConfig {
    export enum source {
        BOUQUET = 'bouquet',
        PER_SERVICE = 'per-service',
    }
}

