import json
import subprocess
import time
import urllib.request
import urllib.error

TOKEN = 'test04'
BASE_URL = 'http://localhost:8089'
ORIGIN = 'https://xg2g.home.matrixcentral.de'

def test_channel(ref, name, category):
    headers = {
        'Authorization': f'Bearer {TOKEN}',
        'Origin': ORIGIN,
        'Content-Type': 'application/json'
    }
    info_data = {
        'serviceRef': ref,
        'capabilities': {
            'capabilitiesVersion': 3,
            'container': ['fmp4', 'hls'],
            'videoCodecs': ['av1', 'hevc', 'h264'],
            'audioCodecs': ['aac'],
            'supportsHls': True,
            'allowTranscode': True,
            'videoCodecSignals': [
                {
                    'codec': 'av1',
                    'isSupported': True,
                    'supported': True,
                    'smooth': True,
                    'powerEfficient': True,
                    'probeSource': 'verified'
                }
            ]
        }
    }
    req = urllib.request.Request(f'{BASE_URL}/api/v3/live/stream-info', data=json.dumps(info_data).encode('utf-8'), headers=headers)
    with urllib.request.urlopen(req) as resp:
        info = json.loads(resp.read().decode('utf-8'))
    
    token = info.get('playbackDecisionToken')
    decision = info.get('decision', {})
    trace = decision.get('trace', {})
    source = trace.get('source', {})
    
    src_codec = source.get('videoCodec', 'h264')
    fps = source.get('fps', 25)
    interlaced = source.get('interlaced', False)
    width = source.get('width', 1920)
    height = source.get('height', 1080)
    res_label = f"{width}x{height} @ {fps}fps ({'1080i50' if interlaced and height==1080 else '720p50' if height==720 else '1080p50'})"
    
    intent_data = {
        'type': 'stream.start',
        'serviceRef': ref,
        'playbackDecisionToken': token,
        'params': {
            'profile': 'av1_hw',
            'intent': 'quality'
        }
    }
    req2 = urllib.request.Request(f'{BASE_URL}/api/v3/intents', data=json.dumps(intent_data).encode('utf-8'), headers=headers)
    with urllib.request.urlopen(req2) as resp2:
        res2 = json.loads(resp2.read().decode('utf-8'))
    sid = res2.get('sessionId')
    
    clean_ref = ref.rstrip(':')
    m3u8_url = f'{BASE_URL}/api/v3/streams/{clean_ref}/playlist.m3u8'
    req3 = urllib.request.Request(m3u8_url, headers={'Authorization': f'Bearer {TOKEN}'})
    try:
        urllib.request.urlopen(req3)
    except Exception:
        pass
    
    time.sleep(5)
    ps_out = subprocess.getoutput('ps aux | grep ffmpeg')
    ffmpeg_lines = [line for line in ps_out.split('\n') if 'ffmpeg' in line and not 'grep' in line and not 'python' in line]
    
    ffmpeg_cmd = "none"
    actual_codec = "none"
    is_passthrough = "nein"
    
    for line in ffmpeg_lines:
        if '/opt/ffmpeg/bin/ffmpeg' in line or 'ffmpeg' in line:
            ffmpeg_cmd = line
            if '-c:v copy' in line:
                actual_codec = "copy"
                is_passthrough = "ja"
            elif 'av1_vaapi' in line:
                actual_codec = "av1_vaapi"
                is_passthrough = "nein"
            elif 'hevc_vaapi' in line:
                actual_codec = "hevc_vaapi"
                is_passthrough = "nein"
            elif 'h264_vaapi' in line:
                actual_codec = "h264_vaapi"
                is_passthrough = "nein"
            elif '-c:v' in line:
                actual_codec = line.split('-c:v')[1].split()[0]
                is_passthrough = "nein"
            break
            
    if sid:
        stop_data = {'type': 'stream.stop', 'serviceRef': ref, 'sessionId': sid}
        req_s = urllib.request.Request(f'{BASE_URL}/api/v3/intents', data=json.dumps(stop_data).encode('utf-8'), headers=headers)
        try:
            urllib.request.urlopen(req_s)
        except Exception:
            pass
            
    return {
        "name": name,
        "category": category,
        "source_codec": src_codec,
        "resolution": res_label,
        "intent": "quality (av1_hw)",
        "planner_decision": "Transcode (HW VAAPI)" if is_passthrough == "nein" else "Passthrough (Copy)",
        "actual_codec": actual_codec,
        "passthrough": is_passthrough,
        "cmd_sample": ffmpeg_cmd[:120] + "..." if len(ffmpeg_cmd) > 120 else ffmpeg_cmd
    }

channels = [
    ('1:0:19:83:6:85:C00000:0:0:0:', 'Sky Cinema Premiere HD', 'GRAIN / Film'),
    ('1:0:19:6B:C:85:C00000:0:0:0:', 'Sky Cinema Classics HD', 'GRAIN / Classic Film'),
    ('1:0:19:8C:4:85:C00000:0:0:0:', 'Warner TV Film HD', 'GRAIN / Film'),
    ('1:0:19:74:4:85:C00000:0:0:0:', 'Sky Cinema Action HD', 'DARK / High Contrast'),
    ('1:0:19:D:6:85:C00000:0:0:0:', 'Sky Crime HD', 'DARK / Series'),
    ('1:0:19:EF7A:3F9:1:C00000:0:0:0:', 'SAT.1 GOLD HD', 'NORMAL / TV Series'),
    ('1:0:19:7B:10:85:C00000:0:0:0:', 'Warner TV Serie HD', 'NORMAL / Drama'),
    ('1:0:19:2B66:3F3:1:C00000:0:0:0:', 'ZDF HD', 'CLEAN_STUDIO / News (720p)'),
    ('1:0:19:132F:3EF:1:C00000:0:0:0:', 'ORF 1 HD', 'CLEAN_STUDIO / Live (720p)')
]

results = []
for ref, name, cat in channels:
    res = test_channel(ref, name, cat)
    results.append(res)
    time.sleep(2)

print(json.dumps(results, indent=2))
