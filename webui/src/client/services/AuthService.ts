/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class AuthService {
    /**
     * Create session cookie
     * Exchanges the Bearer token for a secure HttpOnly session cookie, enabling native playback (HLS) without token in URL.
     * @returns void
     * @throws ApiError
     */
    public static createSession(): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/session',
            errors: {
                401: `Unauthorized`,
            },
        });
    }
}
