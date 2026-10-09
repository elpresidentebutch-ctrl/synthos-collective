@echo off
title SYNTHOS Local Validator (synthos-home-1)
cd /d "%~dp0\.."
echo ==========================================================
echo   Starting SYNTHOS Local Block-Producing Validator Node
echo   Node ID: synthos-home-1
echo   Port: 8091
echo ==========================================================
set SYNTHOS_CONFIG=config/home-validator.json
set SYNTHOS_BLOCK_PRODUCER=true
set SYNTHOS_BLOCK_INTERVAL_SECONDS=10
set SYNTHOS_PRODUCE_EMPTY_BLOCKS=true
set SYNTHOS_PRODUCER_ROTATION=true
set SYNTHOS_PRODUCER_ROUND_SECONDS=30
set SYNTHOS_PRODUCER_ROTATION_LOCK_SECONDS=60
synthosd.exe
pause
