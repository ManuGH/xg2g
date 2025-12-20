/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { DvrCapabilities } from '../models/DvrCapabilities';
import type { RecordingStatus } from '../models/RecordingStatus';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class DvrService {
    /**
     * Get DVR capabilities
     * @returns DvrCapabilities Capabilities
     * @throws ApiError
     */
    public static getDvrCapabilities(): CancelablePromise<DvrCapabilities> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/dvr/capabilities',
        });
    }
    /**
     * Get DVR recording status
     * @returns RecordingStatus Recording status
     * @throws ApiError
     */
    public static getDvrStatus(): CancelablePromise<RecordingStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/dvr/status',
        });
    }
}
