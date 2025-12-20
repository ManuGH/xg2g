/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type Timer = {
    timerId: string;
    serviceRef: string;
    begin: number;
    end: number;
    name: string;
    description?: string;
    serviceName?: string;
    state: Timer.state;
    receiverState?: Record<string, any>;
    createdAt?: string;
    updatedAt?: string;
};
export namespace Timer {
    export enum state {
        SCHEDULED = 'scheduled',
        RECORDING = 'recording',
        COMPLETED = 'completed',
        DISABLED = 'disabled',
        UNKNOWN = 'unknown',
    }
}

