/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Breadcrumb } from './Breadcrumb';
import type { DirectoryItem } from './DirectoryItem';
import type { RecordingItem } from './RecordingItem';
import type { RecordingRoot } from './RecordingRoot';
export type RecordingResponse = {
    roots?: Array<RecordingRoot>;
    current_root?: string;
    current_path?: string;
    breadcrumbs?: Array<Breadcrumb>;
    directories?: Array<DirectoryItem>;
    recordings?: Array<RecordingItem>;
};

