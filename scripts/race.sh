#!/bin/bash
set -e

echo "🏃 Running race condition detection tests..."

# Create race directory
mkdir -p race

# Run tests with race detector
echo "🔬 Running unit tests with race detector..."
go test -race -v ./... > race/race-report.txt 2>&1 || RACE_EXIT_CODE=$?

# Also run specific race-focused tests
echo "🔬 Running concurrent tests..."
go test -race -v -run ".*Concurrent.*|.*Race.*|.*Parallel.*" ./... >> race/race-report.txt 2>&1 || true

# Check for race conditions
RACES_FOUND=$(grep -c "WARNING: DATA RACE" race/race-report.txt || echo "0")
RACE_DETAILS=$(grep -A10 -B2 "WARNING: DATA RACE" race/race-report.txt || echo "No race conditions detected")

echo "🏃 Race condition detection results:"
echo "   Races found: $RACES_FOUND"

# Generate race summary
cat > race/summary.txt << EOF
Race Condition Detection Summary
================================

Races Found: $RACES_FOUND
Status: $(if [ "$RACES_FOUND" -eq 0 ]; then echo "✅ PASS - No race conditions detected"; else echo "❌ FAIL - Race conditions found"; fi)

Generated: $(date)

Details:
--------
$RACE_DETAILS
EOF

cat race/summary.txt

# Run stress tests for concurrency-critical components
echo "💪 Running stress tests..."
go test -race -count=10 ./internal/services/sync/... >> race/stress-report.txt 2>&1 || true
go test -race -count=10 ./internal/storage/... >> race/stress-report.txt 2>&1 || true
go test -race -count=10 ./internal/state/... >> race/stress-report.txt 2>&1 || true

STRESS_RACES=$(grep -c "WARNING: DATA RACE" race/stress-report.txt || echo "0")
echo "   Stress test races: $STRESS_RACES"

# Check if any races were found
TOTAL_RACES=$((RACES_FOUND + STRESS_RACES))

if [ "$TOTAL_RACES" -gt 0 ]; then
    echo "❌ Race conditions detected: $TOTAL_RACES"
    echo "📂 Detailed reports saved in race/ directory"
    echo ""
    echo "🔍 Race condition details:"
    head -100 race/race-report.txt | grep -A20 -B5 "WARNING: DATA RACE" || true
    exit 1
fi

echo "✅ No race conditions detected!"
echo "📂 Reports saved in race/ directory"