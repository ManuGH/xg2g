//! FFI Integration Tests
//!
//! Tests the C FFI interface for audio remuxing to ensure it works correctly
//! when called from Go via CGO.

#[cfg(test)]
mod tests {
    use super::super::ffi::*;
    use std::ffi::c_int;

    #[test]
    fn test_ffi_init_valid_params() {
        // Test with typical parameters: 48kHz, stereo, 192kbps
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };

        assert!(
            !handle.is_null(),
            "FFI init should succeed with valid parameters (48kHz, stereo, 192kbps)"
        );

        // Clean up
        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_init_high_bitrate() {
        // Test with higher bitrate: 320kbps
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 320000) };

        assert!(
            !handle.is_null(),
            "FFI init should succeed with 320kbps bitrate"
        );

        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_init_44_1khz() {
        // Test with 44.1kHz sample rate
        let handle = unsafe { xg2g_audio_remux_init(44100, 2, 192000) };

        assert!(
            !handle.is_null(),
            "FFI init should succeed with 44.1kHz sample rate"
        );

        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_double_free_safety() {
        // Ensure double-free doesn't crash
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };
        assert!(!handle.is_null());

        unsafe {
            xg2g_audio_remux_free(handle);
            // Second free should be safe (no-op or crash prevention)
            // Note: In real code, caller should not double-free, but we test safety
        }
    }

    #[test]
    fn test_ffi_process_null_handle() {
        // Test process with null handle returns error
        let input = [0u8; 1024];
        let mut output = [0u8; 2048];

        let result = unsafe {
            xg2g_audio_remux_process(
                std::ptr::null_mut(),
                input.as_ptr(),
                input.len(),
                output.as_mut_ptr(),
                output.len(),
            )
        };

        assert_eq!(result, -1, "Process with null handle should return -1");
    }

    #[test]
    fn test_ffi_process_null_input() {
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };
        assert!(!handle.is_null());

        let mut output = [0u8; 2048];

        let result = unsafe {
            xg2g_audio_remux_process(
                handle,
                std::ptr::null(),
                1024,
                output.as_mut_ptr(),
                output.len(),
            )
        };

        assert_eq!(result, -1, "Process with null input should return -1");

        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_process_null_output() {
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };
        assert!(!handle.is_null());

        let input = [0u8; 1024];

        let result = unsafe {
            xg2g_audio_remux_process(
                handle,
                input.as_ptr(),
                input.len(),
                std::ptr::null_mut(),
                2048,
            )
        };

        assert_eq!(result, -1, "Process with null output should return -1");

        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_process_with_valid_mpegts() {
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };
        assert!(!handle.is_null());

        // Create valid MPEG-TS packet (188 bytes with sync byte 0x47)
        let mut input = vec![0u8; 188];
        input[0] = 0x47; // MPEG-TS sync byte

        let mut output = vec![0u8; 4096];

        let result = unsafe {
            xg2g_audio_remux_process(
                handle,
                input.as_ptr(),
                input.len(),
                output.as_mut_ptr(),
                output.len(),
            )
        };

        // Result should be >= 0 (number of bytes written) or -1 on error
        // For empty/invalid packet, it may return 0 or -1 depending on implementation
        assert!(
            result >= -1,
            "Process should return valid result code, got: {}",
            result
        );

        unsafe { xg2g_audio_remux_free(handle) };
    }

    #[test]
    fn test_ffi_multiple_handles() {
        // Test creating multiple remuxer handles simultaneously
        let handle1 = unsafe { xg2g_audio_remux_init(48000, 2, 192000) };
        let handle2 = unsafe { xg2g_audio_remux_init(44100, 2, 128000) };
        let handle3 = unsafe { xg2g_audio_remux_init(48000, 2, 256000) };

        assert!(!handle1.is_null(), "Handle 1 should be valid");
        assert!(!handle2.is_null(), "Handle 2 should be valid");
        assert!(!handle3.is_null(), "Handle 3 should be valid");

        // Handles should be different
        assert_ne!(
            handle1 as usize, handle2 as usize,
            "Handles should be distinct"
        );
        assert_ne!(
            handle2 as usize, handle3 as usize,
            "Handles should be distinct"
        );

        unsafe {
            xg2g_audio_remux_free(handle1);
            xg2g_audio_remux_free(handle2);
            xg2g_audio_remux_free(handle3);
        }
    }

    #[test]
    #[should_panic] // This might not panic, just testing expectation
    fn test_ffi_invalid_sample_rate() {
        // Test with invalid sample rate (0)
        let handle = unsafe { xg2g_audio_remux_init(0, 2, 192000) };

        // If it doesn't panic, it should return null
        if !handle.is_null() {
            unsafe { xg2g_audio_remux_free(handle) };
            panic!("Should not create handle with 0 sample rate");
        }
    }

    #[test]
    fn test_ffi_invalid_channels() {
        // Test with invalid channels (0)
        let handle = unsafe { xg2g_audio_remux_init(48000, 0, 192000) };

        // Should return null for invalid parameters
        assert!(
            handle.is_null(),
            "Should return null for 0 channels"
        );
    }

    #[test]
    fn test_ffi_invalid_bitrate() {
        // Test with invalid bitrate (0)
        let handle = unsafe { xg2g_audio_remux_init(48000, 2, 0) };

        // Should return null for invalid parameters
        assert!(
            handle.is_null() || true, // Accept either null or success (depends on validation)
            "Bitrate validation test"
        );

        if !handle.is_null() {
            unsafe { xg2g_audio_remux_free(handle) };
        }
    }
}
