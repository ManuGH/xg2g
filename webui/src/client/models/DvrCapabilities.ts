/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type DvrCapabilities = {
    timers: {
        edit?: boolean;
        delete?: boolean;
        readBackVerify?: boolean;
    };
    conflicts: {
        preview?: boolean;
        receiverAware?: boolean;
    };
    series: {
        supported?: boolean;
        mode?: DvrCapabilities.mode;
        delegatedProvider?: string;
    };
};
export namespace DvrCapabilities {
    export enum mode {
        NONE = 'none',
        DELEGATED = 'delegated',
        MANAGED = 'managed',
    }
}

