/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { SeriesRule } from '../models/SeriesRule';
import type { SeriesRuleRunReport } from '../models/SeriesRuleRunReport';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class SeriesService {
    /**
     * List all series recording rules
     * @returns SeriesRule List of rules
     * @throws ApiError
     */
    public static getSeriesRules(): CancelablePromise<Array<SeriesRule>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/series-rules',
        });
    }
    /**
     * Create a new series rule
     * @param requestBody
     * @returns SeriesRule Created
     * @throws ApiError
     */
    public static createSeriesRule(
        requestBody: SeriesRule,
    ): CancelablePromise<SeriesRule> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/series-rules',
            body: requestBody,
            mediaType: 'application/json',
        });
    }
    /**
     * Run a specific series rule immediately
     * @param id
     * @param trigger
     * @returns SeriesRuleRunReport Run report
     * @throws ApiError
     */
    public static runSeriesRule(
        id: string,
        trigger: string = 'manual',
    ): CancelablePromise<SeriesRuleRunReport> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/series-rules/{id}/run',
            path: {
                'id': id,
            },
            query: {
                'trigger': trigger,
            },
            errors: {
                404: `Rule not found`,
            },
        });
    }
    /**
     * Run all enabled series rules immediately
     * @param trigger
     * @returns SeriesRuleRunReport List of run reports
     * @throws ApiError
     */
    public static runAllSeriesRules(
        trigger: string = 'manual',
    ): CancelablePromise<Array<SeriesRuleRunReport>> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/series-rules/run',
            query: {
                'trigger': trigger,
            },
        });
    }
    /**
     * Delete a series rule
     * @param id
     * @returns void
     * @throws ApiError
     */
    public static deleteSeriesRule(
        id: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/series-rules/{id}',
            path: {
                'id': id,
            },
            errors: {
                404: `Rule not found`,
            },
        });
    }
}
