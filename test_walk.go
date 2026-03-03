package main

import (
"fmt"
"os"
"path/filepath"
)

func main() {
err := filepath.Walk("/root/xg2g/backend/internal/control/http/v3", func(path string, info os.FileInfo, err error) error {
tln(path)
 nil
})
if err != nil {
tln("Error:", err)
}
}
