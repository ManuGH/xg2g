package main
import (
    "os"
    "fmt"
    "github.com/ManuGH/xg2g/internal/domain/session/store"
)
func main() {
    os.Setenv("XG2G_STORAGE", "sqlite")
    _, err := store.OpenBoltStore("tmp/verify-storage/state.db")
    if err != nil {
        fmt.Println("SUCCESS:", err)
        os.Exit(0)
    }
    fmt.Println("FAILURE: Bolt opened while SQLite is Truth")
    os.Exit(1)
}
