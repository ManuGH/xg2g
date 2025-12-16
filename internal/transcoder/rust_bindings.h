/* xg2g Rust Audio Remuxer FFI Bindings
 * C header file for Go CGO integration
 *
 * This header declares the C-compatible functions exported by the Rust
 * transcoder library for native audio remuxing (MP2/AC3 → AAC).
 */

#ifndef XG2G_TRANSCODER_H
#define XG2G_TRANSCODER_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque handle to Rust AudioRemuxer instance */
typedef void* xg2g_remuxer_handle;

/* Initialize audio remuxer
 *
 * Creates a new audio remuxer instance with the specified configuration.
 *
 * Arguments:
 *   sample_rate - Audio sample rate in Hz (e.g., 48000)
 *   channels    - Number of audio channels (e.g., 2 for stereo)
 *   bitrate     - Target AAC bitrate in bits per second (e.g., 192000)
 *
 * Returns:
 *   Opaque handle to remuxer instance, or NULL on error
 *
 * Safety:
 *   The returned handle must be freed with xg2g_audio_remux_free()
 */
xg2g_remuxer_handle xg2g_audio_remux_init(int sample_rate, int channels, int bitrate);

/* Process audio data through the remuxer
 *
 * Processes a chunk of MPEG-TS data, remuxing MP2/AC3 audio to AAC.
 *
 * Arguments:
 *   handle          - Remuxer handle from xg2g_audio_remux_init()
 *   input           - Pointer to input MPEG-TS data (MP2/AC3 audio)
 *   input_len       - Length of input data in bytes
 *   output          - Pointer to output buffer for MPEG-TS data (AAC audio)
 *   output_capacity - Size of output buffer in bytes
 *
 * Returns:
 *   Number of bytes written to output buffer, or -1 on error
 *
 * Safety:
 *   - handle must be valid (from xg2g_audio_remux_init)
 *   - input must point to valid memory of at least input_len bytes
 *   - output must point to valid writable memory of at least output_capacity bytes
 *   - Caller must ensure buffers don't overlap
 */
int xg2g_audio_remux_process(
    xg2g_remuxer_handle handle,
    const uint8_t* input,
    size_t input_len,
    uint8_t* output,
    size_t output_capacity
);

/* Free the audio remuxer and release resources
 *
 * Arguments:
 *   handle - Remuxer handle from xg2g_audio_remux_init()
 *
 * Safety:
 *   - handle must be valid (from xg2g_audio_remux_init)
 *   - handle must not be used after this call
 *   - This function is idempotent (safe to call with NULL)
 */
void xg2g_audio_remux_free(xg2g_remuxer_handle handle);

/* Get transcoder version string
 *
 * Returns:
 *   Pointer to null-terminated version string (static lifetime)
 *
 * Safety:
 *   Returned pointer is valid for the entire program lifetime.
 *   Caller must NOT free this pointer.
 */
const char* xg2g_transcoder_version(void);

/* Get last error message
 *
 * Returns:
 *   Pointer to null-terminated error string, or NULL if no error
 *
 * Safety:
 *   Caller must free the returned string with xg2g_free_string()
 */
char* xg2g_last_error(void);

/* Free a string allocated by Rust
 *
 * Arguments:
 *   s - Pointer to string from Rust functions
 *
 * Safety:
 *   - s must have been allocated by a Rust FFI function
 *   - s must not be used after this call
 */
void xg2g_free_string(char* s);

#ifdef __cplusplus
}
#endif

#endif /* XG2G_TRANSCODER_H */
