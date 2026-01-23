package main
import (
    "database/sql"
    "fmt"
    "os"
    _ "github.com/mattn/go-sqlite3"
)
func main() {
    db, _ := sql.Open("sqlite3", "tmp/verify-storage/capabilities.sqlite")
    defer db.Close()
    var v int
    db.QueryRow("PRAGMA user_version").Scan(&v)
    if v == 2 {
        fmt.Println("OK")
    } else {
        fmt.Printf("FAIL: got %d\n", v)
        os.Exit(1)
    }
}
