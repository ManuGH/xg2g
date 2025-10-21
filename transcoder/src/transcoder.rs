use anyhow::{Context, Result};
use axum::body::Body;
use bytes::Bytes;
use futures::stream::Stream;
use futures::StreamExt;
use serde::{Deserialize, Serialize};
use std::pin::Pin;
use std::process::Stdio;
use std::task::{Context as TaskContext, Poll};
use tokio::io::{AsyncBufReadExt, AsyncRead, AsyncWriteExt, BufReader};
use tokio::process::{Child, Command};
use tokio_util::io::ReaderStream;
use tracing::{debug, error, info, warn};

/// Wrapper that keeps the FFmpeg child process alive while streaming
/// This ensures FFmpeg doesn't get killed when the Child handle is dropped
struct ProcessStream {
    child: Option<Child>,
    stream: ReaderStream<tokio::process::ChildStdout>,
}

impl ProcessStream {
    fn new(mut child: Child, stdout: tokio::process::ChildStdout) -> Self {
        Self {
            child: Some(child),
            stream: ReaderStream::new(stdout),
        }
    }
}

impl Stream for ProcessStream {
    type Item = Result<Bytes, std::io::Error>;

    fn poll_next(mut self: Pin<&mut Self>, cx: &mut TaskContext<'_>) -> Poll<Option<Self::Item>> {
        // Poll the underlying stream
        let result = Pin::new(&mut self.stream).poll_next(cx);

        // If stream ended, wait for child to exit and log result
        if let Poll::Ready(None) = result {
            if let Some(mut child) = self.child.take() {
                // Spawn a task to wait for child exit and log result
                tokio::spawn(async move {
                    match child.wait().await {
                        Ok(status) => {
                            if status.success() {
                                debug!("FFmpeg process exited successfully");
                            } else {
                                warn!("FFmpeg process exited with status: {}", status);
                            }
                        }
                        Err(e) => {
                            error!("Failed to wait for FFmpeg process: {}", e);
                        }
                    }
                });
            }
        }

        result
    }
}

impl Drop for ProcessStream {
    fn drop(&mut self) {
        if let Some(mut child) = self.child.take() {
            // Kill the child process if stream is dropped before completion
            tokio::spawn(async move {
                match child.kill().await {
                    Ok(_) => debug!("FFmpeg process killed on stream drop"),
                    Err(e) => warn!("Failed to kill FFmpeg process: {}", e),
                }
            });
        }
    }
}

/// Configuration for the VAAPI transcoder
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TranscoderConfig {
    /// VAAPI device path (e.g., /dev/dri/renderD128)
    pub vaapi_device: String,

    /// Video codec (e.g., "h264", "hevc")
    pub video_codec: String,

    /// Video bitrate (e.g., "5000k")
    pub video_bitrate: String,

    /// Audio codec (e.g., "aac")
    pub audio_codec: String,

    /// Audio bitrate (e.g., "192k")
    pub audio_bitrate: String,

    /// Audio channels (2 for stereo)
    pub audio_channels: u8,

    /// Analyze duration in microseconds (2000000 = 2s)
    pub analyze_duration: u64,

    /// Probe size in bytes (2000000 = 2MB)
    pub probe_size: u64,

    /// FFmpeg path
    pub ffmpeg_path: String,
}

impl Default for TranscoderConfig {
    fn default() -> Self {
        Self {
            vaapi_device: "/dev/dri/renderD128".to_string(),
            video_codec: "h264".to_string(),
            video_bitrate: "5000k".to_string(),
            audio_codec: "aac".to_string(),
            audio_bitrate: "192k".to_string(),
            audio_channels: 2,
            analyze_duration: 2_000_000, // 2 seconds
            probe_size: 2_000_000,       // 2 MB
            ffmpeg_path: "ffmpeg".to_string(),
        }
    }
}

impl TranscoderConfig {
    /// Load configuration from environment variables
    pub fn from_env() -> Self {
        Self {
            vaapi_device: std::env::var("VAAPI_DEVICE")
                .unwrap_or_else(|_| "/dev/dri/renderD128".to_string()),
            video_codec: std::env::var("VIDEO_CODEC").unwrap_or_else(|_| "h264".to_string()),
            video_bitrate: std::env::var("VIDEO_BITRATE").unwrap_or_else(|_| "5000k".to_string()),
            audio_codec: std::env::var("AUDIO_CODEC").unwrap_or_else(|_| "aac".to_string()),
            audio_bitrate: std::env::var("AUDIO_BITRATE").unwrap_or_else(|_| "192k".to_string()),
            audio_channels: std::env::var("AUDIO_CHANNELS")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(2),
            analyze_duration: std::env::var("ANALYZE_DURATION")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(2_000_000),
            probe_size: std::env::var("PROBE_SIZE")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(2_000_000),
            ffmpeg_path: std::env::var("FFMPEG_PATH").unwrap_or_else(|_| "ffmpeg".to_string()),
        }
    }
}

/// VAAPI hardware transcoder
pub struct VaapiTranscoder {
    config: TranscoderConfig,
}

impl VaapiTranscoder {
    pub fn new(config: TranscoderConfig) -> Self {
        Self { config }
    }

    /// Build FFmpeg command line arguments for VAAPI transcoding
    /// Minimal configuration tested to work reliably with live HTTP streams
    fn build_ffmpeg_args(&self, input: &str) -> Vec<String> {
        vec![
            "-hide_banner".to_string(),
            "-loglevel".to_string(),
            "error".to_string(),
            // Fast stream analysis (reduced for quick startup)
            "-analyzeduration".to_string(),
            self.config.analyze_duration.to_string(),
            "-probesize".to_string(),
            self.config.probe_size.to_string(),
            // Fix Enigma2 timestamp issues
            "-fflags".to_string(),
            "+genpts+igndts+nobuffer".to_string(),
            // Initialize VAAPI device BEFORE input (critical for live streams!)
            "-init_hw_device".to_string(),
            format!("vaapi=va:{}", self.config.vaapi_device),
            // Input
            "-i".to_string(),
            input.to_string(),
            // Video: CPU deinterlace -> GPU encode (minimal, stable config)
            "-vf".to_string(),
            "yadif,format=nv12,hwupload".to_string(),
            "-c:v".to_string(),
            format!("{}_vaapi", self.config.video_codec),
            "-b:v".to_string(),
            self.config.video_bitrate.clone(),
            // Audio: Simple AAC encoding with sync
            "-c:a".to_string(),
            self.config.audio_codec.clone(),
            "-b:a".to_string(),
            self.config.audio_bitrate.clone(),
            "-ac".to_string(),
            self.config.audio_channels.to_string(),
            // Audio/Video sync fixes
            "-async".to_string(),
            "1".to_string(),
            "-vsync".to_string(),
            "1".to_string(),
            "-max_muxing_queue_size".to_string(),
            "9999".to_string(),
            // Output format
            "-f".to_string(),
            "mpegts".to_string(),
            "pipe:1".to_string(),
        ]
    }

    /// Transcode a stream from a URL
    pub async fn transcode_stream(
        &self,
        source_url: &str,
    ) -> Result<Pin<Box<dyn Stream<Item = Result<Bytes, std::io::Error>> + Send>>> {
        let args = self.build_ffmpeg_args(source_url);

        info!("Starting FFmpeg with VAAPI transcoding");
        debug!("FFmpeg command: {} {}", self.config.ffmpeg_path, args.join(" "));

        let mut child = Command::new(&self.config.ffmpeg_path)
            .args(&args)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .context("Failed to spawn FFmpeg process")?;

        let stdout = child
            .stdout
            .take()
            .context("Failed to get FFmpeg stdout")?;

        let stderr = child
            .stderr
            .take()
            .context("Failed to get FFmpeg stderr")?;

        // Log FFmpeg stderr in background
        tokio::spawn(async move {
            let reader = BufReader::new(stderr);
            let mut lines = reader.lines();

            while let Ok(Some(line)) = lines.next_line().await {
                debug!("FFmpeg: {}", line);
            }
        });

        // Create ProcessStream that keeps child alive while streaming
        let stream = ProcessStream::new(child, stdout);

        Ok(Box::pin(stream))
    }

    /// Transcode a stream from stdin (for POST requests with body)
    pub async fn transcode_stdin(
        &self,
        input_body: Body,
    ) -> Result<Pin<Box<dyn Stream<Item = Result<Bytes, std::io::Error>> + Send>>> {
        let args = self.build_ffmpeg_args("pipe:0");

        info!("Starting FFmpeg with VAAPI transcoding (stdin input)");
        debug!("FFmpeg command: {} {}", self.config.ffmpeg_path, args.join(" "));

        let mut child = Command::new(&self.config.ffmpeg_path)
            .args(&args)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .context("Failed to spawn FFmpeg process")?;

        let mut stdin = child.stdin.take().context("Failed to get FFmpeg stdin")?;
        let stdout = child
            .stdout
            .take()
            .context("Failed to get FFmpeg stdout")?;
        let stderr = child
            .stderr
            .take()
            .context("Failed to get FFmpeg stderr")?;

        // Log FFmpeg stderr in background
        tokio::spawn(async move {
            let reader = BufReader::new(stderr);
            let mut lines = reader.lines();

            while let Ok(Some(line)) = lines.next_line().await {
                debug!("FFmpeg: {}", line);
            }
        });

        // Pipe input body to FFmpeg stdin
        tokio::spawn(async move {
            use http_body_util::BodyExt;
            let mut stream = input_body.into_data_stream();

            while let Some(chunk) = stream.next().await {
                match chunk {
                    Ok(bytes) => {
                        if let Err(e) = stdin.write_all(&bytes).await {
                            error!("Error writing to FFmpeg stdin: {}", e);
                            break;
                        }
                    }
                    Err(e) => {
                        error!("Error reading input body: {:?}", e);
                        break;
                    }
                }
            }

            // Close stdin to signal EOF
            drop(stdin);
        });

        // Create stream from stdout
        let stream = tokio_util::io::ReaderStream::new(stdout);

        Ok(Box::pin(stream))
    }
}
