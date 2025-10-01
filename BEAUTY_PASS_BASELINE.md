# Beauty Pass - Baseline before changes

Generated: 2025-09-30T23:20:00Z
Repository: xg2g-1
Current branch: main
Current commit: a3b2924

## Build Status

\n```text
EXIT_CODE=0
```text

## Test Status

\n```text
=== RUN   TestResolveOWISettingsValidation
=== RUN   TestResolveOWISettingsValidation/negative_timeout
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("-1000")
=== RUN   TestResolveOWISettingsValidation/negative_retries
2025/10/01 01:19:31 config: using default for XG2G_OWI_TIMEOUT_MS ("10000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("-1")
=== RUN   TestResolveOWISettingsValidation/negative_backoff
2025/10/01 01:19:31 config: using default for XG2G_OWI_TIMEOUT_MS ("10000")
2025/10/01 01:19:31 config: using default for XG2G_OWI_RETRIES ("3")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("-500")
=== RUN   TestResolveOWISettingsValidation/excessive_timeout
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("120000")
=== RUN   TestResolveOWISettingsValidation/backoff_exceeds_max
2025/10/01 01:19:31 config: using default for XG2G_OWI_TIMEOUT_MS ("10000")
2025/10/01 01:19:31 config: using default for XG2G_OWI_RETRIES ("3")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("40000")
=== RUN   TestResolveOWISettingsValidation/invalid_number_format
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("not-a-number")
=== RUN   TestResolveOWISettingsValidation/defaults_are_valid
2025/10/01 01:19:31 config: using default for XG2G_OWI_TIMEOUT_MS ("10000")
2025/10/01 01:19:31 config: using default for XG2G_OWI_RETRIES ("3")
2025/10/01 01:19:31 config: using default for XG2G_OWI_BACKOFF_MS ("500")
=== RUN   TestResolveOWISettingsValidation/valid_custom_values
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("15000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("5")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("300")
--- PASS: TestResolveOWISettingsValidation (0.00s)
    --- PASS: TestResolveOWISettingsValidation/negative_timeout (0.00s)
    --- PASS: TestResolveOWISettingsValidation/negative_retries (0.00s)
    --- PASS: TestResolveOWISettingsValidation/negative_backoff (0.00s)
    --- PASS: TestResolveOWISettingsValidation/excessive_timeout (0.00s)
    --- PASS: TestResolveOWISettingsValidation/backoff_exceeds_max (0.00s)
    --- PASS: TestResolveOWISettingsValidation/invalid_number_format (0.00s)
    --- PASS: TestResolveOWISettingsValidation/defaults_are_valid (0.00s)
    --- PASS: TestResolveOWISettingsValidation/valid_custom_values (0.00s)
=== RUN   TestMaskURL
--- PASS: TestMaskURL (0.00s)
=== RUN   TestEnvBehavior
2025/10/01 01:19:31 config: using default for XG2G_TEST_KEY ("default")
2025/10/01 01:19:31 config: using XG2G_TEST_KEY from environment ("value")
2025/10/01 01:19:31 config: using default for XG2G_TEST_KEY ("default") because environment variable is empty
2025/10/01 01:19:31 config: using XG2G_API_TOKEN from environment (set)
--- PASS: TestEnvBehavior (0.00s)
=== RUN   TestAtoi
=== PAUSE TestAtoi
=== RUN   TestResolveStreamPort
2025/10/01 01:19:31 config: using default for XG2G_STREAM_PORT ("8001")
2025/10/01 01:19:31 config: using XG2G_STREAM_PORT from environment ("9000")
2025/10/01 01:19:31 config: using XG2G_STREAM_PORT from environment ("not-a-number")
2025/10/01 01:19:31 config: using XG2G_STREAM_PORT from environment ("0")
2025/10/01 01:19:31 config: using XG2G_STREAM_PORT from environment ("70000")
--- PASS: TestResolveStreamPort (0.00s)
=== RUN   TestResolveMetricsListen
2025/10/01 01:19:31 config: using default for XG2G_METRICS_LISTEN ("")
2025/10/01 01:19:31 config: using XG2G_METRICS_LISTEN from environment (":9090")
--- PASS: TestResolveMetricsListen (0.00s)
=== RUN   TestResolveOWISettings_DefaultsAndErrors
2025/10/01 01:19:31 config: using default for XG2G_OWI_TIMEOUT_MS ("10000")
2025/10/01 01:19:31 config: using default for XG2G_OWI_RETRIES ("3")
2025/10/01 01:19:31 config: using default for XG2G_OWI_BACKOFF_MS ("500")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("-1")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("1000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("abc")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("1000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("20")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("1000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("3")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("not-a-number")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("1000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("3")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("0")
2025/10/01 01:19:31 config: using XG2G_OWI_TIMEOUT_MS from environment ("1000")
2025/10/01 01:19:31 config: using XG2G_OWI_RETRIES from environment ("3")
2025/10/01 01:19:31 config: using XG2G_OWI_BACKOFF_MS from environment ("40000")
--- PASS: TestResolveOWISettings_DefaultsAndErrors (0.00s)
=== RUN   TestEnsureDataDir
=== PAUSE TestEnsureDataDir
=== CONT  TestAtoi
--- PASS: TestAtoi (0.00s)
=== CONT  TestEnsureDataDir
2025/10/01 01:19:31 config: data directory "/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestEnsureDataDir490572410/001/nested/dir" does not exist, attempting to create it
2025/10/01 01:19:31 config: successfully created data directory "/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestEnsureDataDir490572410/001/nested/dir"
2025/10/01 01:19:31 config: data directory "/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestEnsureDataDir490572410/001/nested/dir" is valid and writable
2025/10/01 01:19:31 config: data directory "/private/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestEnsureDataDir490572410/001" is valid and writable
2025/10/01 01:19:31 config: data directory "/private/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestEnsureDataDir490572410/001" is valid and writable
--- PASS: TestEnsureDataDir (0.00s)
PASS
ok    github.com/ManuGH/xg2g/cmd/daemon (cached)
=== RUN   TestHandleStatus
{"level":"info","service":"xg2g","version":"","req_id":"11127b55-cab8-4f16-9f1e-b6e5b38fa51e","method":"GET","path":"/api/status","remote_addr":"","user_agent":"","event":"request.handled","status":200,"duration":0.158208,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/api/status","duration_ms":0,"remote_addr":"","req_id":"7bd20620-5029-4325-a863-c69ff0480ff8","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestHandleStatus (0.00s)
=== RUN   TestHandleRefresh_ErrorDoesNotUpdateLastRun
{"level":"info","service":"xg2g","version":"","req_id":"4b2b11f2-2252-4160-8f06-763919a74613","method":"POST","path":"/api/refresh","remote_addr":"","user_agent":"","component":"jobs","event":"refresh.start","time":"2025-09-30T16:32:21+02:00","message":"starting refresh"}
{"level":"error","service":"xg2g","version":"","req_id":"4b2b11f2-2252-4160-8f06-763919a74613","method":"POST","path":"/api/refresh","remote_addr":"","user_agent":"","component":"api","error":"unsupported openwebif base URL scheme \"\"","event":"refresh.failed","method":"POST","duration_ms":0,"status":"error","time":"2025-09-30T16:32:21+02:00","message":"refresh failed"}
{"level":"info","service":"xg2g","version":"","req_id":"4b2b11f2-2252-4160-8f06-763919a74613","method":"POST","path":"/api/refresh","remote_addr":"","user_agent":"","event":"request.handled","status":500,"duration":0.014125,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"POST","path":"/api/refresh","duration_ms":0,"remote_addr":"","req_id":"7dbe5b2f-1f4d-4835-97ae-ba142afd95a6","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestHandleRefresh_ErrorDoesNotUpdateLastRun (0.00s)
=== RUN   TestRecordRefreshMetrics
--- PASS: TestRecordRefreshMetrics (0.00s)
=== RUN   TestHandleRefresh_SuccessUpdatesLastRun
    http_test.go:72: Skipping success test as it requires mocking infrastructure
--- SKIP: TestHandleRefresh_SuccessUpdatesLastRun (0.00s)
=== RUN   TestHandleHealth
{"level":"info","service":"xg2g","version":"","req_id":"4a7ac7a4-0322-4b9c-966a-2078cfb11937","method":"GET","path":"/healthz","remote_addr":"","user_agent":"","event":"request.handled","status":200,"duration":0.012333,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/healthz","duration_ms":0,"remote_addr":"","req_id":"52ff59eb-e679-4955-b9a6-2439a1d7332b","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestHandleHealth (0.00s)
=== RUN   TestHandleReady
{"level":"info","service":"xg2g","version":"","req_id":"f314007e-0b91-48c0-920f-099cd0e211c5","method":"GET","path":"/readyz","remote_addr":"","user_agent":"","event":"request.handled","status":503,"duration":0.022167,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/readyz","duration_ms":0,"remote_addr":"","req_id":"0408e549-98dc-4c19-818e-cc1c48cc585a","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
{"level":"info","service":"xg2g","version":"","req_id":"caf05421-d6fe-4cd1-8014-00bd7db6a5c5","method":"GET","path":"/readyz","remote_addr":"","user_agent":"","event":"request.handled","status":200,"duration":0.061125,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/readyz","duration_ms":0,"remote_addr":"","req_id":"185b293d-490b-4ffa-8899-1ceb715dd5e3","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestHandleReady (0.00s)
=== RUN   TestAuthMiddleware
=== RUN   TestAuthMiddleware/no_token_configured,_fail_closed
{"level":"error","service":"xg2g","version":"","component":"auth","event":"auth.fail_closed","time":"2025-09-30T16:32:21+02:00","message":"XG2G_API_TOKEN is not configured, access denied"}
=== RUN   TestAuthMiddleware/token_configured,_no_header,_unauthorized
{"level":"warn","service":"xg2g","version":"","component":"auth","event":"auth.missing_header","time":"2025-09-30T16:32:21+02:00","message":"authorization header missing"}
=== RUN   TestAuthMiddleware/token_configured,_wrong_token,_forbidden
{"level":"warn","service":"xg2g","version":"","component":"auth","event":"auth.invalid_token","time":"2025-09-30T16:32:21+02:00","message":"invalid api token"}
=== RUN   TestAuthMiddleware/token_configured,_correct_token,_access_granted
--- PASS: TestAuthMiddleware (0.00s)
    --- PASS: TestAuthMiddleware/no_token_configured,_fail_closed (0.00s)
    --- PASS: TestAuthMiddleware/token_configured,_no_header,_unauthorized (0.00s)
    --- PASS: TestAuthMiddleware/token_configured,_wrong_token,_forbidden (0.00s)
    --- PASS: TestAuthMiddleware/token_configured,_correct_token,_access_granted (0.00s)
=== RUN   TestSecureFileHandlerSymlinkPolicy
=== RUN   TestSecureFileHandlerSymlinkPolicy/B6:_valid_file_access
{"level":"info","service":"xg2g","version":"","req_id":"765387b9-f9d3-43d5-b742-7c7e4c769006","method":"GET","path":"/files/test.m3u","remote_addr":"","user_agent":"","component":"api","event":"file_req.allowed","path":"test.m3u","time":"2025-09-30T16:32:21+02:00","message":"serving file"}
{"level":"info","service":"xg2g","version":"","req_id":"765387b9-f9d3-43d5-b742-7c7e4c769006","method":"GET","path":"/files/test.m3u","remote_addr":"","user_agent":"","event":"request.handled","status":200,"duration":1.443458,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/test.m3u","duration_ms":1,"remote_addr":"","req_id":"73308709-eb63-498b-9b0d-39a628254f51","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B7:_subdirectory_file_access
{"level":"info","service":"xg2g","version":"","req_id":"7b1c0002-df70-4d3f-b7ab-8435c8c89c02","method":"GET","path":"/files/subdir/sub.m3u","remote_addr":"","user_agent":"","component":"api","event":"file_req.allowed","path":"subdir/sub.m3u","time":"2025-09-30T16:32:21+02:00","message":"serving file"}
{"level":"info","service":"xg2g","version":"","req_id":"7b1c0002-df70-4d3f-b7ab-8435c8c89c02","method":"GET","path":"/files/subdir/sub.m3u","remote_addr":"","user_agent":"","event":"request.handled","status":200,"duration":0.115875,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/subdir/sub.m3u","duration_ms":0,"remote_addr":"","req_id":"b00c17a2-5339-41bb-b746-6d2141281542","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B8:_symlink_to_outside_file
{"level":"warn","service":"xg2g","version":"","req_id":"84b786d8-6d06-4218-a2f7-72bf1d14c04a","method":"GET","path":"/files/evil_symlink","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"evil_symlink","resolved_path":"/private/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestSecureFileHandlerSymlinkPolicy2063387955/outside/secret.txt","reason":"path_escape","time":"2025-09-30T16:32:21+02:00","message":"path escapes data directory"}
{"level":"info","service":"xg2g","version":"","req_id":"84b786d8-6d06-4218-a2f7-72bf1d14c04a","method":"GET","path":"/files/evil_symlink","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.073458,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/evil_symlink","duration_ms":0,"remote_addr":"","req_id":"6a1eff4a-ecea-492a-ab30-9a601ceeca65","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B9:_symlink_chain_to_outside
{"level":"warn","service":"xg2g","version":"","req_id":"5f3a1123-aaac-4f91-abbf-b1ac13bd150b","method":"GET","path":"/files/link1","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"link1","resolved_path":"/private/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestSecureFileHandlerSymlinkPolicy2063387955/outside/secret.txt","reason":"path_escape","time":"2025-09-30T16:32:21+02:00","message":"path escapes data directory"}
{"level":"info","service":"xg2g","version":"","req_id":"5f3a1123-aaac-4f91-abbf-b1ac13bd150b","method":"GET","path":"/files/link1","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.100125,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/link1","duration_ms":0,"remote_addr":"","req_id":"6cfee935-ea64-4517-9d79-1444606b2075","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B10:_path_traversal_with_..
{"level":"warn","service":"xg2g","version":"","req_id":"22ce6d96-4fe8-4af1-8b5c-655f1ce934db","method":"GET","path":"/files/../outside/secret.txt","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"../outside/secret.txt","reason":"path_escape","time":"2025-09-30T16:32:21+02:00","message":"detected traversal sequence"}
{"level":"info","service":"xg2g","version":"","req_id":"22ce6d96-4fe8-4af1-8b5c-655f1ce934db","method":"GET","path":"/files/../outside/secret.txt","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.005292,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/../outside/secret.txt","duration_ms":0,"remote_addr":"","req_id":"33cbe6ae-0ecf-4658-857b-9b02e0451954","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B11:_symlink_directory_traversal
{"level":"warn","service":"xg2g","version":"","req_id":"d6dc4322-d733-442e-b568-ed21eb60ddbb","method":"GET","path":"/files/evil_dir/secret.txt","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"evil_dir/secret.txt","resolved_path":"/private/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestSecureFileHandlerSymlinkPolicy2063387955/outside/secret.txt","reason":"path_escape","time":"2025-09-30T16:32:21+02:00","message":"path escapes data directory"}
{"level":"info","service":"xg2g","version":"","req_id":"d6dc4322-d733-442e-b568-ed21eb60ddbb","method":"GET","path":"/files/evil_dir/secret.txt","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.058334,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/evil_dir/secret.txt","duration_ms":0,"remote_addr":"","req_id":"43338359-68de-45d9-80e2-d625aff741f8","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/B12:_URL-encoded_traversal_%2e%2e
{"level":"warn","service":"xg2g","version":"","req_id":"0e7f3b52-06bc-4efb-860f-78ee0cea9918","method":"GET","path":"/files/../outside/secret.txt","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"../outside/secret.txt","reason":"path_escape","time":"2025-09-30T16:32:21+02:00","message":"detected traversal sequence"}
{"level":"info","service":"xg2g","version":"","req_id":"0e7f3b52-06bc-4efb-860f-78ee0cea9918","method":"GET","path":"/files/../outside/secret.txt","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.006875,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/../outside/secret.txt","duration_ms":0,"remote_addr":"","req_id":"56351d1e-3b0e-47ab-8622-ec2e8bb65d39","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/directory_access_blocked
{"level":"warn","service":"xg2g","version":"","req_id":"5323c9bc-0dd3-4589-b8f3-e77ca0ce883a","method":"GET","path":"/files/subdir/","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"subdir/","reason":"directory_listing","time":"2025-09-30T16:32:21+02:00","message":"directory listing forbidden"}
{"level":"info","service":"xg2g","version":"","req_id":"5323c9bc-0dd3-4589-b8f3-e77ca0ce883a","method":"GET","path":"/files/subdir/","remote_addr":"","user_agent":"","event":"request.handled","status":403,"duration":0.011417,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/subdir/","duration_ms":0,"remote_addr":"","req_id":"09cc0291-2a0b-4b8b-b633-9d8f0813f580","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/nonexistent_file
{"level":"info","service":"xg2g","version":"","req_id":"9031d6e5-a4b5-4eab-95c6-7d341013e723","method":"GET","path":"/files/nonexistent.txt","remote_addr":"","user_agent":"","component":"api","event":"file_req.not_found","path":"/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestSecureFileHandlerSymlinkPolicy2063387955/data/nonexistent.txt","time":"2025-09-30T16:32:21+02:00","message":"file not found"}
{"level":"info","service":"xg2g","version":"","req_id":"9031d6e5-a4b5-4eab-95c6-7d341013e723","method":"GET","path":"/files/nonexistent.txt","remote_addr":"","user_agent":"","event":"request.handled","status":404,"duration":0.026083,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/files/nonexistent.txt","duration_ms":0,"remote_addr":"","req_id":"d0061115-6445-458a-9cce-7ebe97c67dde","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestSecureFileHandlerSymlinkPolicy/method_not_allowed
{"level":"warn","service":"xg2g","version":"","req_id":"173f65ef-aca1-4fd8-bee2-cbb2a3d72df2","method":"POST","path":"/files/test.m3u","remote_addr":"","user_agent":"","component":"api","event":"file_req.denied","path":"test.m3u","reason":"method_not_allowed","time":"2025-09-30T16:32:21+02:00","message":"method not allowed"}
{"level":"info","service":"xg2g","version":"","req_id":"173f65ef-aca1-4fd8-bee2-cbb2a3d72df2","method":"POST","path":"/files/test.m3u","remote_addr":"","user_agent":"","event":"request.handled","status":405,"duration":0.005167,"time":"2025-09-30T16:32:21+02:00","message":"http request"}
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"POST","path":"/files/test.m3u","duration_ms":0,"remote_addr":"","req_id":"1f6fa44e-8bd0-49d2-b600-df9ccccac288","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestSecureFileHandlerSymlinkPolicy (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B6:_valid_file_access (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B7:_subdirectory_file_access (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B8:_symlink_to_outside_file (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B9:_symlink_chain_to_outside (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B10:_path_traversal_with_.. (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B11:_symlink_directory_traversal (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/B12:_URL-encoded_traversal_%2e%2e (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/directory_access_blocked (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/nonexistent_file (0.00s)
    --- PASS: TestSecureFileHandlerSymlinkPolicy/method_not_allowed (0.00s)
=== RUN   TestMiddlewareChain
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/test","duration_ms":0,"remote_addr":"192.0.2.1","req_id":"79c7a2ea-7557-48d6-a8b4-f1fa389628fb","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestMiddlewareChain (0.00s)
=== RUN   TestRequestIDMiddleware
=== RUN   TestRequestIDMiddleware/generates_new_request_ID_when_none_provided
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/test","duration_ms":0,"remote_addr":"192.0.2.1","req_id":"2790052c-b2b3-40e4-981f-b329d12b50f2","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
=== RUN   TestRequestIDMiddleware/uses_existing_request_ID_from_header
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/test","duration_ms":0,"remote_addr":"192.0.2.1","req_id":"test-request-id-123","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestRequestIDMiddleware (0.00s)
    --- PASS: TestRequestIDMiddleware/generates_new_request_ID_when_none_provided (0.00s)
    --- PASS: TestRequestIDMiddleware/uses_existing_request_ID_from_header (0.00s)
=== RUN   TestRequestIDMiddlewareLogging
{"level":"info","service":"xg2g","version":"","component":"api","event":"request.complete","method":"GET","path":"/api/test","duration_ms":0,"remote_addr":"192.0.2.1","req_id":"7b65587e-96d1-4e1b-843f-38f1e129c6df","time":"2025-09-30T16:32:21+02:00","message":"request completed"}
--- PASS: TestRequestIDMiddlewareLogging (0.00s)
=== RUN   TestClientIP
=== RUN   TestClientIP/direct_connection
=== RUN   TestClientIP/invalid_remote_addr
--- PASS: TestClientIP (0.00s)
    --- PASS: TestClientIP/direct_connection (0.00s)
    --- PASS: TestClientIP/invalid_remote_addr (0.00s)
=== RUN   TestSecurityHeadersMiddleware
--- PASS: TestSecurityHeadersMiddleware (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/api (cached)
=== RUN   TestXMLTVParsingRoundTrip
=== RUN   TestXMLTVParsingRoundTrip/single_channel_basic
=== RUN   TestXMLTVParsingRoundTrip/multiple_channels_with_icons
=== RUN   TestXMLTVParsingRoundTrip/channel_with_multiple_names
=== RUN   TestXMLTVParsingRoundTrip/special_characters_in_names
=== RUN   TestXMLTVParsingRoundTrip/empty_channel_list
=== RUN   TestXMLTVParsingRoundTrip/channel_with_empty_display_name
--- PASS: TestXMLTVParsingRoundTrip (0.04s)
    --- PASS: TestXMLTVParsingRoundTrip/single_channel_basic (0.01s)
    --- PASS: TestXMLTVParsingRoundTrip/multiple_channels_with_icons (0.01s)
    --- PASS: TestXMLTVParsingRoundTrip/channel_with_multiple_names (0.01s)
    --- PASS: TestXMLTVParsingRoundTrip/special_characters_in_names (0.01s)
    --- PASS: TestXMLTVParsingRoundTrip/empty_channel_list (0.01s)
    --- PASS: TestXMLTVParsingRoundTrip/channel_with_empty_display_name (0.01s)
=== RUN   TestXMLTVErrorCases
=== RUN   TestXMLTVErrorCases/invalid_directory
=== RUN   TestXMLTVErrorCases/readonly_directory
--- PASS: TestXMLTVErrorCases (0.00s)
    --- PASS: TestXMLTVErrorCases/invalid_directory (0.00s)
    --- PASS: TestXMLTVErrorCases/readonly_directory (0.00s)
=== RUN   TestChannelIDGeneration
=== RUN   TestChannelIDGeneration/simple_name
=== RUN   TestChannelIDGeneration/name_with_spaces
=== RUN   TestChannelIDGeneration/special_characters
=== RUN   TestChannelIDGeneration/umlauts_and_special
=== RUN   TestChannelIDGeneration/empty_string
=== RUN   TestChannelIDGeneration/only_special_chars
=== RUN   TestChannelIDGeneration/consecutive_separators
--- PASS: TestChannelIDGeneration (0.00s)
    --- PASS: TestChannelIDGeneration/simple_name (0.00s)
    --- PASS: TestChannelIDGeneration/name_with_spaces (0.00s)
    --- PASS: TestChannelIDGeneration/special_characters (0.00s)
    --- PASS: TestChannelIDGeneration/umlauts_and_special (0.00s)
    --- PASS: TestChannelIDGeneration/empty_string (0.00s)
    --- PASS: TestChannelIDGeneration/only_special_chars (0.00s)
    --- PASS: TestChannelIDGeneration/consecutive_separators (0.00s)
=== RUN   TestXMLTVStructValidation
=== RUN   TestXMLTVStructValidation/xml_structure_validity
--- PASS: TestXMLTVStructValidation (0.01s)
    --- PASS: TestXMLTVStructValidation/xml_structure_validity (0.01s)
=== RUN   TestFindBest
=== RUN   TestFindBest/exact_match
=== RUN   TestFindBest/fuzzy_match_distance_1
=== RUN   TestFindBest/fuzzy_match_distance_2
=== RUN   TestFindBest/no_match_exceeds_distance
=== RUN   TestFindBest/empty_input
=== RUN   TestFindBest/case_insensitive_match
=== RUN   TestFindBest/fuzzy_match_max_distance_0
=== RUN   TestFindBest/numeric_channel_match
--- PASS: TestFindBest (0.00s)
    --- PASS: TestFindBest/exact_match (0.00s)
    --- PASS: TestFindBest/fuzzy_match_distance_1 (0.00s)
    --- PASS: TestFindBest/fuzzy_match_distance_2 (0.00s)
    --- PASS: TestFindBest/no_match_exceeds_distance (0.00s)
    --- PASS: TestFindBest/empty_input (0.00s)
    --- PASS: TestFindBest/case_insensitive_match (0.00s)
    --- PASS: TestFindBest/fuzzy_match_max_distance_0 (0.00s)
    --- PASS: TestFindBest/numeric_channel_match (0.00s)
=== RUN   TestLevenshtein
=== RUN   TestLevenshtein/identical_strings
=== RUN   TestLevenshtein/one_insertion
=== RUN   TestLevenshtein/one_deletion
=== RUN   TestLevenshtein/one_substitution
=== RUN   TestLevenshtein/empty_strings
=== RUN   TestLevenshtein/one_empty_string
=== RUN   TestLevenshtein/other_empty_string
=== RUN   TestLevenshtein/completely_different
=== RUN   TestLevenshtein/unicode_characters
=== RUN   TestLevenshtein/long_strings
--- PASS: TestLevenshtein (0.00s)
    --- PASS: TestLevenshtein/identical_strings (0.00s)
    --- PASS: TestLevenshtein/one_insertion (0.00s)
    --- PASS: TestLevenshtein/one_deletion (0.00s)
    --- PASS: TestLevenshtein/one_substitution (0.00s)
    --- PASS: TestLevenshtein/empty_strings (0.00s)
    --- PASS: TestLevenshtein/one_empty_string (0.00s)
    --- PASS: TestLevenshtein/other_empty_string (0.00s)
    --- PASS: TestLevenshtein/completely_different (0.00s)
    --- PASS: TestLevenshtein/unicode_characters (0.00s)
    --- PASS: TestLevenshtein/long_strings (0.00s)
=== RUN   TestNameKey
=== RUN   TestNameKey/lowercase_conversion
=== RUN   TestNameKey/already_lowercase
=== RUN   TestNameKey/mixed_case
=== RUN   TestNameKey/empty_string
=== RUN   TestNameKey/with_numbers
=== RUN   TestNameKey/unicode_characters
--- PASS: TestNameKey (0.00s)
    --- PASS: TestNameKey/lowercase_conversion (0.00s)
    --- PASS: TestNameKey/already_lowercase (0.00s)
    --- PASS: TestNameKey/mixed_case (0.00s)
    --- PASS: TestNameKey/empty_string (0.00s)
    --- PASS: TestNameKey/with_numbers (0.00s)
    --- PASS: TestNameKey/unicode_characters (0.00s)
=== RUN   TestGenerateXMLTV
--- PASS: TestGenerateXMLTV (0.00s)
=== RUN   TestWriteXMLTV
--- PASS: TestWriteXMLTV (0.01s)
=== RUN   TestXMLStructureValidation
--- PASS: TestXMLStructureValidation (0.00s)
=== RUN   TestTitleOmitEmptyLang
--- PASS: TestTitleOmitEmptyLang (0.00s)
=== RUN   TestGoldenXMLTV
--- PASS: TestGoldenXMLTV (0.00s)
=== RUN   TestGoldenFiles
=== RUN   TestGoldenFiles/multi_channel_with_icons
=== RUN   TestGoldenFiles/empty_channels
--- PASS: TestGoldenFiles (0.01s)
    --- PASS: TestGoldenFiles/multi_channel_with_icons (0.01s)
    --- PASS: TestGoldenFiles/empty_channels (0.01s)
=== RUN   TestXMLTVBenchmark
    golden_test.go:125: Generated XMLTV for 1000 channels in 6.757291ms
    golden_test.go:133: Generated file size: 86906 bytes
--- PASS: TestXMLTVBenchmark (0.01s)
=== RUN   FuzzParseChannelName
=== RUN   FuzzParseChannelName/seed#0
=== RUN   FuzzParseChannelName/seed#1
=== RUN   FuzzParseChannelName/seed#2
=== RUN   FuzzParseChannelName/seed#3
=== RUN   FuzzParseChannelName/seed#4
=== RUN   FuzzParseChannelName/seed#5
=== RUN   FuzzParseChannelName/seed#6
=== RUN   FuzzParseChannelName/seed#7
=== RUN   FuzzParseChannelName/seed#8
=== RUN   FuzzParseChannelName/seed#9
=== RUN   FuzzParseChannelName/seed#10
=== RUN   FuzzParseChannelName/seed#11
--- PASS: FuzzParseChannelName (0.00s)
    --- PASS: FuzzParseChannelName/seed#0 (0.00s)
    --- PASS: FuzzParseChannelName/seed#1 (0.00s)
    --- PASS: FuzzParseChannelName/seed#2 (0.00s)
    --- PASS: FuzzParseChannelName/seed#3 (0.00s)
    --- PASS: FuzzParseChannelName/seed#4 (0.00s)
    --- PASS: FuzzParseChannelName/seed#5 (0.00s)
    --- PASS: FuzzParseChannelName/seed#6 (0.00s)
    --- PASS: FuzzParseChannelName/seed#7 (0.00s)
    --- PASS: FuzzParseChannelName/seed#8 (0.00s)
    --- PASS: FuzzParseChannelName/seed#9 (0.00s)
    --- PASS: FuzzParseChannelName/seed#10 (0.00s)
    --- PASS: FuzzParseChannelName/seed#11 (0.00s)
=== RUN   FuzzXMLTVGeneration
=== RUN   FuzzXMLTVGeneration/seed#0
=== RUN   FuzzXMLTVGeneration/seed#1
=== RUN   FuzzXMLTVGeneration/seed#2
--- PASS: FuzzXMLTVGeneration (0.02s)
    --- PASS: FuzzXMLTVGeneration/seed#0 (0.01s)
    --- PASS: FuzzXMLTVGeneration/seed#1 (0.01s)
    --- PASS: FuzzXMLTVGeneration/seed#2 (0.01s)
=== RUN   FuzzLevenshtein
=== RUN   FuzzLevenshtein/seed#0
=== RUN   FuzzLevenshtein/seed#1
=== RUN   FuzzLevenshtein/seed#2
=== RUN   FuzzLevenshtein/seed#3
--- PASS: FuzzLevenshtein (0.00s)
    --- PASS: FuzzLevenshtein/seed#0 (0.00s)
    --- PASS: FuzzLevenshtein/seed#1 (0.00s)
    --- PASS: FuzzLevenshtein/seed#2 (0.00s)
    --- PASS: FuzzLevenshtein/seed#3 (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/epg (cached)
=== RUN   TestMakeStableIDFromSRef_Properties
--- PASS: TestMakeStableIDFromSRef_Properties (0.00s)
=== RUN   TestRefresh_IntegrationSuccess
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51453","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":1,"max_attempts":1,"duration_ms":1,"error_class":"ok","status":200,"time":"2025-10-01T00:32:59+02:00","message":"openwebif request completed"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51453","event":"openwebif.bouquets","count":1,"time":"2025-10-01T00:32:59+02:00","message":"fetched bouquets"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51453","event":"openwebif.request","operation":"services.flat","method":"GET","endpoint":"/api/getallservices","attempt":1,"max_attempts":1,"duration_ms":0,"error_class":"ok","status":200,"time":"2025-10-01T00:32:59+02:00","message":"openwebif request completed"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51453","event":"openwebif.services","bouquet_ref":"1:7:***EF:","count":2,"time":"2025-10-01T00:32:59+02:00","message":"fetched services via flat endpoint"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"playlist.write","path":"/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestRefresh_IntegrationSuccess1878132549/001/playlist.m3u","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"playlist written"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"xmltv.success","path":"/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestRefresh_IntegrationSuccess1878132549/001/xmltv.xml","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"XMLTV generated"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.success","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"refresh completed"}
--- PASS: TestRefresh_IntegrationSuccess (0.02s)
=== RUN   TestRefreshWithClient_Success
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"playlist.write","path":"/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestRefreshWithClient_Success1799027250/001/playlist.m3u","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"playlist written"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"xmltv.success","path":"xmltv.xml","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"XMLTV generated"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.success","channels":2,"time":"2025-10-01T00:32:59+02:00","message":"refresh completed"}
--- PASS: TestRefreshWithClient_Success (0.01s)
=== RUN   TestRefreshWithClient_BouquetNotFound
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
--- PASS: TestRefreshWithClient_BouquetNotFound (0.00s)
=== RUN   TestRefreshWithClient_ServicesError
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"playlist.write","path":"/var/folders/99/p3_dnc890xbcy_8qdw4024wh0000gn/T/TestRefreshWithClient_ServicesError1184804176/001/playlist.m3u","channels":0,"time":"2025-10-01T00:32:59+02:00","message":"playlist written"}
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.success","channels":0,"time":"2025-10-01T00:32:59+02:00","message":"refresh completed"}
--- PASS: TestRefreshWithClient_ServicesError (0.00s)
=== RUN   TestRefresh_InvalidStreamPort
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
--- PASS: TestRefresh_InvalidStreamPort (0.00s)
=== RUN   TestRefresh_ConfigValidation
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
--- PASS: TestRefresh_ConfigValidation (0.00s)
=== RUN   TestRefresh_M3UWriteError
{"level":"info","service":"xg2g","version":"","component":"jobs","event":"refresh.start","time":"2025-10-01T00:32:59+02:00","message":"starting refresh"}
--- PASS: TestRefresh_M3UWriteError (0.00s)
=== RUN   TestValidateConfig
=== RUN   TestValidateConfig/http_ok
=== RUN   TestValidateConfig/https_ok
=== RUN   TestValidateConfig/empty_owiabase
=== RUN   TestValidateConfig/missing_scheme
=== RUN   TestValidateConfig/unsupported_scheme
=== RUN   TestValidateConfig/invalid_port
=== RUN   TestValidateConfig/port_too_large
=== RUN   TestValidateConfig/empty_datadir
=== RUN   TestValidateConfig/path_traversal
=== RUN   TestValidateConfig/invalid_stream_port_zero
=== RUN   TestValidateConfig/invalid_stream_port_negative
=== RUN   TestValidateConfig/missing_host
--- PASS: TestValidateConfig (0.00s)
    --- PASS: TestValidateConfig/http_ok (0.00s)
    --- PASS: TestValidateConfig/https_ok (0.00s)
    --- PASS: TestValidateConfig/empty_owiabase (0.00s)
    --- PASS: TestValidateConfig/missing_scheme (0.00s)
    --- PASS: TestValidateConfig/unsupported_scheme (0.00s)
    --- PASS: TestValidateConfig/invalid_port (0.00s)
    --- PASS: TestValidateConfig/port_too_large (0.00s)
    --- PASS: TestValidateConfig/empty_datadir (0.00s)
    --- PASS: TestValidateConfig/path_traversal (0.00s)
    --- PASS: TestValidateConfig/invalid_stream_port_zero (0.00s)
    --- PASS: TestValidateConfig/invalid_stream_port_negative (0.00s)
    --- PASS: TestValidateConfig/missing_host (0.00s)
=== RUN   TestMakeStableIDFromSRef
--- PASS: TestMakeStableIDFromSRef (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/jobs  (cached)
=== RUN   TestContextWithRequestID
=== RUN   TestContextWithRequestID/nil_context
=== RUN   TestContextWithRequestID/background_context
=== RUN   TestContextWithRequestID/empty_request_ID
--- PASS: TestContextWithRequestID (0.00s)
    --- PASS: TestContextWithRequestID/nil_context (0.00s)
    --- PASS: TestContextWithRequestID/background_context (0.00s)
    --- PASS: TestContextWithRequestID/empty_request_ID (0.00s)
=== RUN   TestContextWithJobID
=== RUN   TestContextWithJobID/nil_context
=== RUN   TestContextWithJobID/background_context
--- PASS: TestContextWithJobID (0.00s)
    --- PASS: TestContextWithJobID/nil_context (0.00s)
    --- PASS: TestContextWithJobID/background_context (0.00s)
=== RUN   TestRequestIDFromContextEmpty
=== RUN   TestRequestIDFromContextEmpty/nil_context
=== RUN   TestRequestIDFromContextEmpty/context_without_request_ID
=== RUN   TestRequestIDFromContextEmpty/context_with_wrong_type
--- PASS: TestRequestIDFromContextEmpty (0.00s)
    --- PASS: TestRequestIDFromContextEmpty/nil_context (0.00s)
    --- PASS: TestRequestIDFromContextEmpty/context_without_request_ID (0.00s)
    --- PASS: TestRequestIDFromContextEmpty/context_with_wrong_type (0.00s)
=== RUN   TestWithContext
--- PASS: TestWithContext (0.00s)
=== RUN   TestWithComponentFromContext
--- PASS: TestWithComponentFromContext (0.00s)
=== RUN   TestBase
--- PASS: TestBase (0.00s)
=== RUN   TestDerive
--- PASS: TestDerive (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/log (cached)
=== RUN   TestClientBouquets5xx
{"level":"error","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51062","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":1,"max_attempts":1,"duration_ms":1,"error_class":"http_5xx","status":502,"time":"2025-09-30T16:23:46+02:00","message":"openwebif request failed"}
--- PASS: TestClientBouquets5xx (0.00s)
=== RUN   TestClientBouquetsInvalidJSON
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51064","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":1,"max_attempts":1,"duration_ms":0,"error_class":"ok","status":200,"time":"2025-09-30T16:23:46+02:00","message":"openwebif request completed"}
{"level":"error","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51064","error":"invalid character 'n' looking for beginning of object key string","event":"openwebif.decode","operation":"bouquets","time":"2025-09-30T16:23:46+02:00","message":"failed to decode bouquets response"}
--- PASS: TestClientBouquetsInvalidJSON (0.00s)
=== RUN   TestClientGetServicesTimeout
{"level":"warn","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51066","event":"openwebif.services","bouquet_ref":"1:***:0","time":"2025-09-30T16:23:47+02:00","message":"no services found for bouquet"}
--- PASS: TestClientGetServicesTimeout (2.50s)
=== RUN   TestClientGetServicesBouquetNotFoundSchemaOK
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51070","event":"openwebif.request","operation":"services.flat","method":"GET","endpoint":"/api/getallservices","attempt":1,"max_attempts":1,"duration_ms":0,"error_class":"ok","status":200,"time":"2025-09-30T16:23:48+02:00","message":"openwebif request completed"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51070","event":"openwebif.request","operation":"services.nested","method":"GET","endpoint":"/api/getservices","attempt":1,"max_attempts":1,"duration_ms":0,"error_class":"ok","status":200,"time":"2025-09-30T16:23:48+02:00","message":"openwebif request completed"}
{"level":"warn","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51070","event":"openwebif.services","bouquet_ref":"1:***:0","time":"2025-09-30T16:23:48+02:00","message":"no services found for bouquet"}
--- PASS: TestClientGetServicesBouquetNotFoundSchemaOK (0.00s)
=== RUN   TestHardenedClient_ResponseHeaderTimeout
--- PASS: TestHardenedClient_ResponseHeaderTimeout (0.10s)
=== RUN   TestStreamURLScenarios
=== RUN   TestStreamURLScenarios/configured_port
=== RUN   TestStreamURLScenarios/existing_port_preserved
=== RUN   TestStreamURLScenarios/ipv6_without_port
=== RUN   TestStreamURLScenarios/ipv6_with_port
=== RUN   TestStreamURLScenarios/trailing_slash
=== RUN   TestStreamURLScenarios/env_fallback
=== RUN   TestStreamURLScenarios/env_invalid_falls_back_to_default
--- PASS: TestStreamURLScenarios (0.00s)
    --- PASS: TestStreamURLScenarios/configured_port (0.00s)
    --- PASS: TestStreamURLScenarios/existing_port_preserved (0.00s)
    --- PASS: TestStreamURLScenarios/ipv6_without_port (0.00s)
    --- PASS: TestStreamURLScenarios/ipv6_with_port (0.00s)
    --- PASS: TestStreamURLScenarios/trailing_slash (0.00s)
    --- PASS: TestStreamURLScenarios/env_fallback (0.00s)
    --- PASS: TestStreamURLScenarios/env_invalid_falls_back_to_default (0.00s)
=== RUN   TestBouquetsTimeout
--- PASS: TestBouquetsTimeout (0.20s)
=== RUN   TestBouquetsRetrySuccess
{"level":"warn","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51077","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":1,"max_attempts":2,"duration_ms":0,"error_class":"http_5xx","status":502,"time":"2025-09-30T16:23:49+02:00","message":"openwebif request retry"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51077","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":2,"max_attempts":2,"duration_ms":0,"error_class":"ok","status":200,"time":"2025-09-30T16:23:49+02:00","message":"openwebif request completed"}
{"level":"info","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51077","event":"openwebif.bouquets","count":1,"time":"2025-09-30T16:23:49+02:00","message":"fetched bouquets"}
--- PASS: TestBouquetsRetrySuccess (0.01s)
=== RUN   TestBouquetsRetryFailure
{"level":"warn","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51079","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":1,"max_attempts":2,"duration_ms":0,"error_class":"http_5xx","status":502,"time":"2025-09-30T16:23:49+02:00","message":"openwebif request retry"}
{"level":"error","service":"xg2g","version":"","component":"openwebif","host":"127.0.0.1:51079","event":"openwebif.request","operation":"bouquets","method":"GET","endpoint":"/api/bouquets","attempt":2,"max_attempts":2,"duration_ms":0,"error_class":"http_5xx","status":502,"time":"2025-09-30T16:23:49+02:00","message":"openwebif request failed"}
--- PASS: TestBouquetsRetryFailure (0.01s)
=== RUN   TestContextCancellationCleanup
--- PASS: TestContextCancellationCleanup (0.20s)
=== RUN   TestPiconURL
=== RUN   TestPiconURL/basic_url
=== RUN   TestPiconURL/base_with_trailing_slash
=== RUN   TestPiconURL/sref_with_special_chars
=== RUN   TestPiconURL/empty_sref
=== RUN   TestPiconURL/ipv6_address
--- PASS: TestPiconURL (0.00s)
    --- PASS: TestPiconURL/basic_url (0.00s)
    --- PASS: TestPiconURL/base_with_trailing_slash (0.00s)
    --- PASS: TestPiconURL/sref_with_special_chars (0.00s)
    --- PASS: TestPiconURL/empty_sref (0.00s)
    --- PASS: TestPiconURL/ipv6_address (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/openwebif (cached)
=== RUN   TestWriteM3UTable
=== RUN   TestWriteM3UTable/basic_with_logo_and_channel_number
=== RUN   TestWriteM3UTable/missing_logo_keeps_stable_tvg-id
--- PASS: TestWriteM3UTable (0.00s)
    --- PASS: TestWriteM3UTable/basic_with_logo_and_channel_number (0.00s)
    --- PASS: TestWriteM3UTable/missing_logo_keeps_stable_tvg-id (0.00s)
PASS
ok    github.com/ManuGH/xg2g/internal/playlist  (cached)
?     github.com/ManuGH/xg2g/test [no test files]
EXIT_CODE=0
```text

## Linter Status (golangci-lint)

\n```text
golangci-lint not installed
```text

## File Count

- Go files: 1404
- Test files: 357
- Markdown files: 64
- YAML files: 98

## Existing Files Check

- [x] LICENSE: EXISTS
- [x] SECURITY.md: EXISTS
- [x] .editorconfig: EXISTS
- [x] CONTRIBUTING.md: EXISTS
- [x] CHANGELOG.md: EXISTS
- [x] .env.example: EXISTS

## Quick Scan Results

\n```text
gofmt (files needing format):

goimports (files needing import fix):
goimports not installed

markdownlint issues:
markdownlint not installed

yamllint issues:
yamllint not installed

GitHub Actions floating tags (@main/@master):
No @main tags
No @master tags
```text

