#!/bin/bash
export XG2G_DATA=/data
export XG2G_V3_E2_HOST=http://10.10.55.64
export XG2G_API_TOKEN=dev-token
export XG2G_BOUQUET=Premium
export XG2G_V3_DVR_WINDOW=2700
export XG2G_OWI_USER=
export XG2G_DEV=true
export XG2G_V3_SHADOW_INTENTS=false
export XG2G_INITIAL_REFRESH=false
export XG2G_V3_HLS_ROOT=/data/v3-hls
export XG2G_V3_STORE_PATH=/data/v3-store
export XG2G_LOG_LEVEL=debug
export XG2G_USE_RUST_REMUXER=true
export XG2G_OWI_PASS=
export XG2G_LISTEN=:8088
export XG2G_V3_FFMPEG_KILL_TIMEOUT=5s
export XG2G_V3_CONFIG_STRICT=false
export XG2G_V3_TUNE_TIMEOUT=10s
export XG2G_V3_FFMPEG_BIN=ffmpeg
export XG2G_V3_WORKER_MODE=standard
export XG2G_V3_TUNER_SLOTS=0,1,2
export XG2G_RATELIMIT_RPS=100
export XG2G_TRANSCODER_URL=
export XG2G_API_TOKEN_SCOPES=*
export XG2G_USE_WEBIF_STREAMS=true
export XG2G_OWI_BASE=http://10.10.55.64
export XG2G_RATELIMIT_ENABLED=false
export XG2G_H264_STREAM_REPAIR=true
export XG2G_V3_STORE_BACKEND=memory
export XG2G_V3_WORKER_ENABLED=true
export XG2G_V3_SHADOW_TARGET=
export LD_LIBRARY_PATH=/app/lib
export XG2G_FUZZY_MAX=2

# New variable
export XG2G_V3_IDLE_TIMEOUT=30s

echo "Starting xg2g_new with XG2G_V3_IDLE_TIMEOUT=30s..."
./xg2g_new
