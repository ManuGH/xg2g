/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class EpgService {
    /**
     * Get EPG data
     * @param from Start timestamp (unix seconds)
     * @param to End timestamp (unix seconds)
     * @param bouquet Filter by bouquet name
     * @param q Filter by search query
     * @returns any EPG data
     * @throws ApiError
     */
    public static getEpg(
        from?: number,
        to?: number,
        bouquet?: string,
        q?: string,
    ): CancelablePromise<{
        items?: Array<{
            service_ref?: string;
            title?: string;
            desc?: string;
            start?: number;
            end?: number;
        }>;
    }> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/epg',
            query: {
                'from': from,
                'to': to,
                'bouquet': bouquet,
                'q': q,
            },
        });
    }
}
