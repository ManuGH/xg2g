#!/usr/bin/env python3
# scripts/ci_gate_zip_purity.py
import zipfile
import sys
import os

def check_purity(zip_path):
    print(f"Checking ZIP purity for: {zip_path}")
    if not os.path.exists(zip_path):
        print(f"Error: File {zip_path} not found.")
        sys.exit(1)

    binary_magics = {
        b'\x7fELF': 'ELF (Linux)',
        b'MZ': 'PE (Windows)',
        b'\xcf\xfa\xed\xfe': 'Mach-O 64-bit (macOS)',
        b'\xce\xfa\xed\xfe': 'Mach-O 32-bit (macOS)',
        b'\xca\xfe\xba\xbe': 'Universal Binary / Java Class (macOS/JVM)'
    }

    banned_substrings = ['__MACOSX', '.DS_Store']
    required_paths = ['xg2g-main/docs/review/walkthrough.md', 'xg2g-main/docs/review/task.md']
    found_banned = []
    missing_required = set(required_paths)
    found_binaries = []

    try:
        with zipfile.ZipFile(zip_path, 'r') as zf:
            namelist = zf.namelist()
            for name in namelist:
                # Check for banned content
                for banned in banned_substrings:
                    if banned in name:
                        found_banned.append(name)
                
                # Check for required content
                if name in missing_required:
                    missing_required.remove(name)

            for info in zf.infolist():
                if info.is_dir():
                    continue
                
                # We skip very small files that might coincidentally have "MZ" or similar
                if info.file_size < 4:
                    continue

                with zf.open(info.filename) as f:
                    header = f.read(4)
                    for magic, desc in binary_magics.items():
                        if header.startswith(magic):
                            # Special case: 'MZ' is common, but 'PE' usually follows in Windows binaries
                            if magic == b'MZ' and info.file_size < 100:
                                continue # Likely not a real PE
                            
                            found_binaries.append((info.filename, desc))
                            break
    except Exception as e:
        print(f"Error reading ZIP: {e}")
        sys.exit(1)

    failed = False

    if found_banned:
        print("❌ FAIL: Banned files/folders detected in review artifact:")
        for name in found_banned[:5]: # Limit output
            print(f"  - {name}")
        if len(found_banned) > 5:
            print(f"  ... and {len(found_banned)-5} more.")
        failed = True

    if missing_required:
        print("❌ FAIL: Required artifacts missing from review artifact:")
        for name in missing_required:
            print(f"  - {name}")
        failed = True

    if found_binaries:
        print("❌ FAIL: Binary executables detected in review artifact:")
        for name, desc in found_binaries:
            print(f"  - {name} ({desc})")
        failed = True
        
    if failed:
        sys.exit(1)

    print("✅ PASS: ZIP purity and artifact check passed.")
    sys.exit(0)

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: ci_gate_zip_purity.py <path_to_zip>")
        sys.exit(1)
    check_purity(sys.argv[1])
