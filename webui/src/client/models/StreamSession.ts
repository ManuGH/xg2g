/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type StreamSession = {
    id?: string;
    client_ip?: string;
    channel_name?: string;
    started_at?: string;
    state?: StreamSession.state;
};
export namespace StreamSession {
    export enum state {
        ACTIVE = 'active',
        IDLE = 'idle',
    }
}

