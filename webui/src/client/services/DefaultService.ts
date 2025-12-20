/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AppConfig } from '../models/AppConfig';
import type { ConfigUpdate } from '../models/ConfigUpdate';
import type { LogEntry } from '../models/LogEntry';
import type { Service } from '../models/Service';
import type { StreamSession } from '../models/StreamSession';
import type { SystemHealth } from '../models/SystemHealth';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class DefaultService {
    /**
     * Get system health
     * @returns SystemHealth Health status
     * @throws ApiError
     */
    public static getSystemHealth(): CancelablePromise<SystemHealth> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/system/health',
        });
    }
    /**
     * Get system configuration
     * @returns AppConfig Configuration
     * @throws ApiError
     */
    public static getSystemConfig(): CancelablePromise<AppConfig> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/system/config',
        });
    }
    /**
     * Update system configuration
     * @param requestBody
     * @returns any Configuration updated
     * @throws ApiError
     */
    public static putSystemConfig(
        requestBody: ConfigUpdate,
    ): CancelablePromise<{
        restart_required?: boolean;
    }> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/system/config',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Invalid configuration`,
                500: `Failed to save configuration`,
            },
        });
    }
    /**
     * Trigger data refresh (EPG/Channels)
     * @returns any Refresh started
     * @throws ApiError
     */
    public static postSystemRefresh(): CancelablePromise<any> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/system/refresh',
            errors: {
                409: `Refresh already in progress`,
            },
        });
    }
    /**
     * List all services (channels)
     * @param bouquet Filter by bouquet name
     * @returns Service List of services
     * @throws ApiError
     */
    public static getServices(
        bouquet?: string,
    ): CancelablePromise<Array<Service>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/services',
            query: {
                'bouquet': bouquet,
            },
        });
    }
    /**
     * Toggle service enabled state
     * @param id
     * @param requestBody
     * @returns any Status updated
     * @throws ApiError
     */
    public static postServicesToggle(
        id: string,
        requestBody: {
            enabled?: boolean;
        },
    ): CancelablePromise<any> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/services/{id}/toggle',
            path: {
                'id': id,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `Service not found`,
            },
        });
    }
    /**
     * List active streams
     * @returns StreamSession Active sessions
     * @throws ApiError
     */
    public static getStreams(): CancelablePromise<Array<StreamSession>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/streams',
        });
    }
    /**
     * Terminate a stream session
     * @param id
     * @returns void
     * @throws ApiError
     */
    public static deleteStreamsId(
        id: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/streams/{id}',
            path: {
                'id': id,
            },
            errors: {
                404: `Session not found`,
            },
        });
    }
    /**
     * Get recent logs
     * @returns LogEntry Recent log entries
     * @throws ApiError
     */
    public static getLogs(): CancelablePromise<Array<LogEntry>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/logs',
        });
    }
}
