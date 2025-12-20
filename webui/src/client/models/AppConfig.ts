/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EPGConfig } from './EPGConfig';
import type { FeatureFlags } from './FeatureFlags';
import type { OpenWebIFConfig } from './OpenWebIFConfig';
import type { PiconsConfig } from './PiconsConfig';
export type AppConfig = {
    version?: string;
    dataDir?: string;
    logLevel?: string;
    openWebIF?: OpenWebIFConfig;
    bouquets?: Array<string>;
    epg?: EPGConfig;
    picons?: PiconsConfig;
    featureFlags?: FeatureFlags;
};

