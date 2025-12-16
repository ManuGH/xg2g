/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Bouquet } from '../models/Bouquet';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class ServicesService {
    /**
     * List all bouquets
     * @returns Bouquet List of bouquets
     * @throws ApiError
     */
    public static getServicesBouquets(): CancelablePromise<Array<Bouquet>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/services/bouquets',
        });
    }
}
