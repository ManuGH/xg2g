#!/bin/bash
set -e

# verify-v3-dto-casing.sh
# Scans v3 DTOs for camelCase compliance in JSON tags.

TARGET_FILE="internal/control/http/v3/server_gen.go"

if [ ! -f "$TARGET_FILE" ]; then
    echo "❌ Error: $TARGET_FILE not found. Run 'make generate' first."
    exit 1
fi

echo "--- Checking v3 JSON Tags for camelCase Compliance ---"

EXIT_CODE=0

# Use awk to find struct field definitions with JSON tags and underscores
# This pattern looks for: Field Name Type `json:"name_with_underscore..."`
RESULTS=$(awk '
    /type [A-Za-z0-9]+ struct \{/ { current_struct = $2 }
    /json:"[^"]*_[^"]*"/ {
        # NR is line number
        # $1 is usually the field name if it is not a comment line
        if ($1 ~ /^\/\//) next;
        
        # Match the tag specifically
        match($0, /json:"[^"]*"/)
        tag_val = substr($0, RSTART, RLENGTH)
        
        # Field name is the first token on the line
        field_name = $1
        
        print NR ":" current_struct "." field_name ":" tag_val
    }
' "$TARGET_FILE" || true)

if [ -n "$RESULTS" ]; then
    while IFS= read -r line; do
        # Format: LINE_NUM:SYMBOL:TAG
        # Use awk to split by the first two colons to handle colons in the tag
        LINE_NUM=$(echo "$line" | awk -F: '{print $1}')
        SYMBOL=$(echo "$line" | awk -F: '{print $2}')
        # Everything from the third field onwards is the tag
        TAG=$(echo "$line" | awk -F: '{for(i=3;i<=NF;i++) printf "%s%s", $i, (i==NF?"":":")}')
        
        echo "❌ Hygiene Violation in $TARGET_FILE at line $LINE_NUM"
        echo "   Symbol: $SYMBOL"
        echo "   Offending Tag: $TAG"
        EXIT_CODE=1
    done <<< "$RESULTS"
else
    echo "✅ All v3 JSON tags are compliant."
fi

exit $EXIT_CODE
