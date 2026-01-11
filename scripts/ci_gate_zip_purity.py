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

    found_binaries = []

    try:
        with zipfile.ZipFile(zip_path, 'r') as zf:
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

    if found_binaries:
        print("❌ FAIL: Binary executables detected in review artifact:")
        for name, desc in found_binaries:
            print(f"  - {name} ({desc})")
        sys.exit(1)

    print("✅ PASS: No binary executables found in review artifact.")
    sys.exit(0)

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: ci_gate_zip_purity.py <path_to_zip>")
        sys.exit(1)
    check_purity(sys.argv[1])
