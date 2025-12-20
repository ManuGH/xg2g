/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { RecordingResponse } from '../models/RecordingResponse';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class RecordingsService {
    /**
     * Browse recordings
     * @param root Root location ID
     * @param path Relative path
     * @returns RecordingResponse Recordings list
     * @throws ApiError
     */
    public static getRecordings(
        root?: string,
        path?: string,
    ): CancelablePromise<RecordingResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/recordings',
            query: {
                'root': root,
                'path': path,
            },
        });
    }
    /**
     * Get HLS playlist for a recording
     * @param recordingId URL-encoded service reference or ID of the recording
     * @returns binary HLS Playlist
     * @throws ApiError
     */
    public static getRecordingHlsPlaylist(
        recordingId: string,
    ): CancelablePromise<Blob> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/recordings/{recordingId}/playlist.m3u8',
            path: {
                'recordingId': recordingId,
            },
            errors: {
                404: `Recording not found`,
            },
        });
    }
    /**
     * Get HLS segment for a recording
     * @param recordingId
     * @param segment
     * @returns binary Media Segment
     * @throws ApiError
     */
    public static getRecordingHlsCustomSegment(
        recordingId: string,
        segment: string,
    ): CancelablePromise<Blob> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/recordings/{recordingId}/{segment}',
            path: {
                'recordingId': recordingId,
                'segment': segment,
            },
        });
    }
}
