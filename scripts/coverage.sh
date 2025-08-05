#!/bin/bash
set -e

echo "🧪 Running tests with coverage..."

# Clean previous coverage
rm -f coverage.out coverage.html coverage-business.out

# Create coverage directory
mkdir -p coverage

# Run tests with coverage
echo "📊 Running unit tests..."
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

# Generate HTML report
echo "📈 Generating HTML coverage report..."
go tool cover -html=coverage.out -o coverage/coverage.html

# Check overall coverage threshold
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
echo "📊 Total coverage: $COVERAGE%"

# Business logic coverage (services, crypto, sync)
echo "📊 Calculating business logic coverage..."
go tool cover -func=coverage.out | grep -E "(services|crypto|sync)" > coverage/business-functions.txt || true

if [ -s coverage/business-functions.txt ]; then
    BUSINESS_COV=$(awk '{sum+=$3; count++} END {if(count>0) print sum/count; else print 0}' coverage/business-functions.txt)
    echo "📊 Business logic coverage: ${BUSINESS_COV}%"
else
    BUSINESS_COV=0
    echo "⚠️  No business logic functions found for coverage calculation"
fi

# Package-by-package coverage
echo "📊 Package coverage breakdown:"
go tool cover -func=coverage.out | grep -E "^github.com/yourusername/obsync/internal" | \
    awk '{
        split($1, parts, "/")
        pkg = parts[length(parts)]
        gsub(/\.go:.*/, "", pkg)
        if (pkg != prev_pkg) {
            if (prev_pkg != "") print prev_pkg ": " prev_cov "%"
            prev_pkg = pkg
            prev_cov = $3
        }
    }
    END { if (prev_pkg != "") print prev_pkg ": " prev_cov "%" }' | \
    sort > coverage/package-coverage.txt

cat coverage/package-coverage.txt

# Generate coverage summary
cat > coverage/summary.txt << EOF
Coverage Summary
================

Total Coverage: ${COVERAGE}%
Business Logic Coverage: ${BUSINESS_COV}%

Thresholds:
- Total: 85% ($(if (( $(echo "$COVERAGE >= 85" | bc -l) )); then echo "✅ PASS"; else echo "❌ FAIL"; fi))
- Business Logic: 80% ($(if (( $(echo "$BUSINESS_COV >= 80" | bc -l) )); then echo "✅ PASS"; else echo "❌ FAIL"; fi))

Generated: $(date)
EOF

cat coverage/summary.txt

# Check thresholds
if (( $(echo "$COVERAGE < 85" | bc -l) )); then
    echo "❌ Total coverage ($COVERAGE%) is below 85% threshold"
    exit 1
fi

if (( $(echo "$BUSINESS_COV < 80" | bc -l) )); then
    echo "❌ Business logic coverage ($BUSINESS_COV%) is below 80% threshold"
    exit 1
fi

echo "✅ Coverage requirements met!"
echo "📂 Reports saved in coverage/ directory"

# Optional: Open coverage report in browser (uncomment if desired)
# if command -v open >/dev/null 2>&1; then
#     open coverage/coverage.html
# elif command -v xdg-open >/dev/null 2>&1; then
#     xdg-open coverage/coverage.html
# fi