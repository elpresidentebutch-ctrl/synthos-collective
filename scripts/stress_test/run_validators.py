#!/usr/bin/env python3
"""Run two validator UI servers (desktop and mobile) for a short stress test.

This script starts a simple HTTP server in each validator directory, opens the
pages in the default web browser, runs for a configurable duration, and then
shuts everything down.

Usage:
    python run_validators.py --duration 300  # run for 5 minutes
"""

import argparse, subprocess, sys, time, os, webbrowser, signal

BASE_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))  # repo root
DESKTOP_DIR = os.path.join(BASE_DIR, "workers", "desktop-validator")
MOBILE_DIR = os.path.join(BASE_DIR, "workers", "mobile-validator")

def start_server(path, port):
    # Launch "python -m http.server <port>" in the given directory
    proc = subprocess.Popen([sys.executable, "-m", "http.server", str(port)], cwd=path, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return proc

def open_page(port, path):
    url = f"http://localhost:{port}/{path}"
    webbrowser.open_new_tab(url)

def main():
    parser = argparse.ArgumentParser(description="Launch desktop and mobile validator UIs for stress testing.")
    parser.add_argument("--duration", type=int, default=300, help="How many seconds to keep the servers running (default 300).")
    args = parser.parse_args()

    print("Starting desktop validator server on port 8001 …")
    desktop_proc = start_server(DESKTOP_DIR, 8001)
    print("Starting mobile validator server on port 8002 …")
    mobile_proc = start_server(MOBILE_DIR, 8002)

    # Give servers a moment to start
    time.sleep(2)
    print("Opening validator pages in the default browser …")
    open_page(8001, "index.html")
    open_page(8002, "index.html")

    try:
        print(f"Running for {args.duration} seconds. Press Ctrl+C to stop early.")
        time.sleep(args.duration)
    except KeyboardInterrupt:
        print("Interrupted by user – shutting down servers.")
    finally:
        for proc, name in [(desktop_proc, "desktop"), (mobile_proc, "mobile")]:
            if proc.poll() is None:
                proc.send_signal(signal.SIGTERM)
                proc.wait(timeout=5)
                print(f"{name.capitalize()} server stopped.")

if __name__ == "__main__":
    main()
