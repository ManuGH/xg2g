/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Timer } from '../models/Timer';
import type { TimerConflictPreviewRequest } from '../models/TimerConflictPreviewRequest';
import type { TimerConflictPreviewResponse } from '../models/TimerConflictPreviewResponse';
import type { TimerCreateRequest } from '../models/TimerCreateRequest';
import type { TimerList } from '../models/TimerList';
import type { TimerPatchRequest } from '../models/TimerPatchRequest';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class TimersService {
    /**
     * List all timers
     * @param state
     * @param from
     * @returns TimerList List of timers
     * @throws ApiError
     */
    public static getTimers(
        state: string = 'scheduled',
        from?: number,
    ): CancelablePromise<TimerList> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/timers',
            query: {
                'state': state,
                'from': from,
            },
        });
    }
    /**
     * Create a timer
     * @param requestBody
     * @returns Timer Timer created
     * @throws ApiError
     */
    public static addTimer(
        requestBody: TimerCreateRequest,
    ): CancelablePromise<Timer> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/timers',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                409: `Duplicate timer`,
                422: `Conflict or validation error`,
                502: `Receiver inconsistent (verification failed)`,
            },
        });
    }
    /**
     * Get timer
     * @param timerId
     * @returns Timer Timer details
     * @throws ApiError
     */
    public static getTimer(
        timerId: string,
    ): CancelablePromise<Timer> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/timers/{timerId}',
            path: {
                'timerId': timerId,
            },
            errors: {
                404: `Timer not found`,
            },
        });
    }
    /**
     * Edit timer
     * @param timerId
     * @param requestBody
     * @returns Timer Timer updated
     * @throws ApiError
     */
    public static updateTimer(
        timerId: string,
        requestBody: TimerPatchRequest,
    ): CancelablePromise<Timer> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/timers/{timerId}',
            path: {
                'timerId': timerId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `Timer not found`,
                409: `Duplicate resulting from edit`,
                422: `Conflict`,
                502: `Receiver fail / Rollback`,
            },
        });
    }
    /**
     * Delete timer
     * @param timerId
     * @returns void
     * @throws ApiError
     */
    public static deleteTimer(
        timerId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/timers/{timerId}',
            path: {
                'timerId': timerId,
            },
            errors: {
                404: `Not found`,
            },
        });
    }
    /**
     * Preview conflicts
     * @param requestBody
     * @returns TimerConflictPreviewResponse Preview result
     * @throws ApiError
     */
    public static previewConflicts(
        requestBody: TimerConflictPreviewRequest,
    ): CancelablePromise<TimerConflictPreviewResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/timers/conflicts:preview',
            body: requestBody,
            mediaType: 'application/json',
        });
    }
}
