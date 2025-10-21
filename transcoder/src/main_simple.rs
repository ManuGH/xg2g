// Ultra-simple test binary to verify execution and logging
use std::io::Write;

fn main() {
    // Direct stderr output - should always work
    eprintln!("[TEST] Binary started successfully!");

    // Direct stdout output
    println!("[TEST] This is stdout");

    // Flush to ensure output
    std::io::stderr().flush().unwrap();
    std::io::stdout().flush().unwrap();

    eprintln!("[TEST] Sleeping for 30 seconds...");
    std::thread::sleep(std::time::Duration::from_secs(30));

    eprintln!("[TEST] Exiting normally");
}
