#!/bin/bash

echo "=== Testing Phase 3: Slippage Input ==="
echo
echo "Prerequisites:"
echo "1. Set SOLANA_PRIVATE_KEY environment variable"
echo "   export SOLANA_PRIVATE_KEY=your_private_key_here"
echo
echo "2. Run the command with -execute flag:"
echo "   go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1 -side buy -execute"
echo
echo "Expected flow:"
echo "1. Program calculates and shows the quote"
echo "2. Shows swap confirmation dialog"
echo "3. After confirming with 'y', asks for slippage tolerance"
echo "4. Accepts values like: '0.5', '1', '2.5', '0.5%', '1%' etc."
echo "5. Default is 0.5% if you just press Enter"
echo "6. Shows final swap parameters including:"
echo "   - Slippage Tolerance"
echo "   - Expected Out"
echo "   - Minimum Out (calculated with slippage)"
echo
echo "Test scenarios to try:"
echo "- Press Enter for default (0.5%)"
echo "- Enter '1' for 1%"
echo "- Enter '0.1' for 0.1%"
echo "- Enter '100' (should work - max allowed)"
echo "- Enter '101' (should fail - over max)"
echo "- Enter '-1' (should fail - negative)"
echo "- Enter 'abc' (should fail - invalid format)"