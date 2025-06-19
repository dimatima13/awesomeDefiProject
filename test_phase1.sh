#!/bin/bash

echo "=== Testing Phase 1 & 2: Wallet Loading and Quote Confirmation ==="
echo

# Test 1: Run without SOLANA_PRIVATE_KEY set
echo "Test 1: Running without private key (should work for quote only)"
go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1 -side buy

echo
echo "Test 2: Running with -execute flag but no private key (should fail)"
go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1 -side buy -execute

echo
echo "Test 3: Running with valid private key and execute flag"
echo "Please set SOLANA_PRIVATE_KEY environment variable with a valid Solana private key"
echo "Example: export SOLANA_PRIVATE_KEY=your_private_key_here"
echo
echo "Then run: go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1 -side buy -execute"
echo
echo "This will show the quote and ask for confirmation (y/n)"
echo
echo "Phase 2 Test: The program should now:"
echo "1. Calculate and show the quote"
echo "2. Display a swap confirmation dialog with:"
echo "   - Pool address"
echo "   - Operation type (BUY/SELL)"
echo "   - Amount In with token symbol"
echo "   - Expected Out with token symbol"
echo "   - Price in SOL per token"
echo "3. Ask for user confirmation (y/n)"
echo "4. If 'n' - cancel the swap"
echo "5. If 'y' - proceed (will show message about next phases)"