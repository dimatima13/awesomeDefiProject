#!/bin/bash

echo "=== Testing Full Swap Execution (Phases 1-6) ==="
echo
echo "⚠️  WARNING: This will execute a real swap on Solana mainnet!"
echo "Make sure you have:"
echo "1. A valid Solana private key in SOLANA_PRIVATE_KEY environment variable"
echo "2. Sufficient SOL balance for the swap and fees"
echo "3. For sell operations - the token you want to sell"
echo
echo "Prerequisites:"
echo "export SOLANA_PRIVATE_KEY=your_private_key_here"
echo
echo "Example commands:"
echo
echo "1. Buy 0.001 SOL worth of a token:"
echo "   go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 0.001 -side buy -execute"
echo
echo "2. Sell tokens for SOL (need to specify correct amount):"
echo "   go run main.go -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1000 -side sell -execute"
echo
echo "The swap will:"
echo "1. Load your wallet from SOLANA_PRIVATE_KEY"
echo "2. Get a quote for the swap"
echo "3. Ask for confirmation (y/n)"
echo "4. Ask for slippage tolerance (default 0.5%)"
echo "5. Build the transaction with:"
echo "   - Creating ATAs if needed"
echo "   - Wrapping/unwrapping SOL for WSOL"
echo "   - Executing the swap instruction"
echo "6. Send and confirm the transaction"
echo "7. Display the transaction hash and explorer link"
echo
echo "Common pools to test with (SOL pairs):"
echo "- USDC-SOL: EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
echo "- RAY-SOL: AVs9TA4nWDzfPJE9gGVNJMVhcQy3V9PGazuz33BfG2RA"
echo "- BONK-SOL: 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2"