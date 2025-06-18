# Raydium V4 AMM Quote Tool - Pure On-Chain Version

This is a pure on-chain implementation that reads pool data directly from Solana blockchain without using any external APIs.

## Key Differences from API Version

1. **No external dependencies** - Works purely with blockchain data
2. **Pool discovery** - Uses `getProgramAccounts` to find all pools
3. **Slower but more decentralized** - Takes 10-30 seconds to find pools
4. **Automatic version detection** - Tries multiple data structure offsets

## Usage

```bash
# Using pool address directly
go run main_onchain.go -pool <POOL_ADDRESS> -amount <AMOUNT> -side <buy|sell>

# Finding best pool for token (slower, uses getProgramAccounts)
go run main_onchain.go -token <TOKEN_ADDRESS> -amount <AMOUNT> -side <buy|sell>
```

## Examples

```bash
# Buy RAY with 1 SOL (finds best pool)
go run main_onchain.go -token 4k3Dyjzvzp8eMZWUXbBCjEvwSkkk59S5iCNLY3QrkX6R -amount 1 -side buy

# Use specific pool
go run main_onchain.go -pool AVs9TA4nWDzfPJE9gGVNJMVhcQy3V9PGazuz33BfG2RA -amount 1 -side buy

# Sell 100 USDC for SOL
go run main_onchain.go -token EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v -amount 100 -side sell
```

## How It Works

1. **Pool Discovery** (when using -token):
   - Calls `getProgramAccounts` on Raydium V4 program
   - Filters for pools containing the token paired with SOL/WSOL
   - Selects pool with highest SOL reserves

2. **Data Parsing**:
   - Reads pool account data directly from blockchain
   - Tries multiple offset configurations for different pool versions
   - Fetches token decimals from mint accounts

3. **Quote Calculation**:
   - Uses constant product AMM formula (x * y = k)
   - Applies 0.25% trading fee
   - All calculations done with on-chain data

## Performance Notes

- Pool discovery takes 10-30 seconds due to `getProgramAccounts`
- Direct pool queries are fast (<1 second)
- Consider caching pool addresses for frequently used tokens

## Advantages

✅ Fully decentralized - no API dependencies  
✅ Always up-to-date with blockchain state  
✅ Works with all Raydium V4 pools  
✅ Transparent data source

## Limitations

⚠️ Slower pool discovery  
⚠️ Higher RPC usage (costs more credits)  
⚠️ Requires understanding of pool data structures