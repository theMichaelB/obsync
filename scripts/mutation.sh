#!/bin/bash
set -e

echo "🧬 Running mutation tests..."

# Install go-mutesting if needed
if ! command -v go-mutesting &> /dev/null; then
    echo "📦 Installing go-mutesting..."
    go install github.com/zimmski/go-mutesting/cmd/go-mutesting@latest
fi

# Create mutation directory
mkdir -p mutation

# Run mutation tests on business logic
echo "🧬 Running mutation tests on business logic..."
go-mutesting ./internal/services/... ./internal/crypto/... ./internal/models/... \
    --exec "go test" \
    --blacklist ".*_test.go" \
    --timeout 30s \
    > mutation/mutation-report.txt 2>&1 || true

# Check if mutation report was generated
if [ ! -f mutation/mutation-report.txt ]; then
    echo "❌ Mutation testing failed to generate report"
    exit 1
fi

# Parse results
SCORE=$(grep -o "Score: [0-9.]*" mutation/mutation-report.txt | cut -d' ' -f2 || echo "0")
MUTANTS_TOTAL=$(grep -o "Mutants: [0-9]*" mutation/mutation-report.txt | cut -d' ' -f2 || echo "0")
MUTANTS_KILLED=$(grep -o "Killed: [0-9]*" mutation/mutation-report.txt | cut -d' ' -f2 || echo "0")
MUTANTS_SURVIVED=$(grep -o "Survived: [0-9]*" mutation/mutation-report.txt | cut -d' ' -f2 || echo "0")

echo "🧬 Mutation testing results:"
echo "   Score: $SCORE"
echo "   Total mutants: $MUTANTS_TOTAL"
echo "   Killed: $MUTANTS_KILLED"
echo "   Survived: $MUTANTS_SURVIVED"

# Generate mutation summary
cat > mutation/summary.txt << EOF
Mutation Testing Summary
========================

Score: $SCORE
Total Mutants: $MUTANTS_TOTAL
Killed: $MUTANTS_KILLED
Survived: $MUTANTS_SURVIVED

Threshold: 0.8 ($(if (( $(echo "$SCORE >= 0.8" | bc -l) )); then echo "✅ PASS"; else echo "❌ FAIL"; fi))

Generated: $(date)
EOF

cat mutation/summary.txt

# Show survived mutants for analysis
echo ""
echo "🔍 Survived mutants (may need additional test cases):"
grep -A5 -B5 "SURVIVED" mutation/mutation-report.txt | head -50 || echo "No survived mutants details found"

# Check threshold
if (( $(echo "$SCORE < 0.8" | bc -l) )); then
    echo "❌ Mutation score ($SCORE) is below 80% threshold"
    echo "💡 Consider adding more test cases to kill survived mutants"
    exit 1
fi

echo "✅ Mutation testing passed!"
echo "📂 Reports saved in mutation/ directory"