package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Protocol being used
const PROTOCOL = "Raydium V4 AMM (Pure On-Chain)"

// Raydium V4 AMM Program ID
var RAYDIUM_AMM_V4 = solana.MustPublicKeyFromBase58("675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8")

// Token decimal constants
const (
	SOL_DECIMALS  = 9
	WSOL_DECIMALS = 9
)

// Known SOL/WSOL addresses
var (
	SOL_MINT  = solana.SolMint
	WSOL_MINT = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
)

type QuoteParams struct {
	PoolAddress  string
	TokenAddress string // Alternative to PoolAddress
	Amount       float64
	Side         string // "buy" or "sell"
}

// OnChainPool represents pool data parsed from on-chain
type OnChainPool struct {
	Address       solana.PublicKey
	BaseMint      solana.PublicKey
	QuoteMint     solana.PublicKey
	BaseVault     solana.PublicKey
	QuoteVault    solana.PublicKey
	BaseAmount    uint64
	QuoteAmount   uint64
	BaseDecimals  uint8
	QuoteDecimals uint8
}

func main() {
	var poolAddr string
	var tokenAddr string
	var amount float64
	var side string

	flag.StringVar(&poolAddr, "pool", "", "Pool address")
	flag.StringVar(&tokenAddr, "token", "", "Token address (finds best pool)")
	flag.Float64Var(&amount, "amount", 0, "Amount to swap")
	flag.StringVar(&side, "side", "", "buy or sell")
	flag.Parse()

	if amount == 0 || side == "" {
		fmt.Println("Usage: go run main_onchain.go [-pool POOL | -token TOKEN] -amount AMOUNT -side buy|sell")
		flag.PrintDefaults()
		return
	}

	if poolAddr == "" && tokenAddr == "" {
		log.Fatal("Either -pool or -token must be specified")
	}

	if side != "buy" && side != "sell" {
		log.Fatal("Side must be 'buy' or 'sell'")
	}

	ctx := context.Background()
	client := rpc.New("https://mainnet.helius-rpc.com/?api-key=4a5313a6-8380-4882-ad4e-e745ec00d629")

	var poolAddress string

	// If token address is provided, find pools
	if tokenAddr != "" {
		pool, err := findPoolsOnChain(ctx, client, tokenAddr)
		if err != nil {
			log.Fatal(err)
		}
		poolAddress = pool.Address.String()
		fmt.Printf("Found pool: %s\n", poolAddress)
	} else {
		poolAddress = poolAddr
	}

	quote, err := calculateQuoteOnChain(ctx, client, QuoteParams{
		PoolAddress: poolAddress,
		Amount:      amount,
		Side:        side,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n=== QUOTE RESULT ===\n")
	fmt.Printf("Protocol: %s\n", PROTOCOL)
	fmt.Printf("Pool: %s\n", poolAddress)
	fmt.Printf("Operation: %s\n", strings.ToUpper(side))
	fmt.Printf("Amount In: %.9f\n", amount)
	fmt.Printf("Expected Out: %.9f\n", quote)
	fmt.Printf("====================\n")
}

// findPoolsOnChain uses getProgramAccounts to find all pools for a token
func findPoolsOnChain(ctx context.Context, client *rpc.Client, tokenAddress string) (*OnChainPool, error) {
	tokenPubkey, err := solana.PublicKeyFromBase58(tokenAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid token address: %w", err)
	}

	fmt.Println("Searching for pools on-chain using getProgramAccounts...")
	fmt.Println("This may take 10-30 seconds...")

	// Create filters to find pools containing our token
	// We'll search for pools where either baseMint or quoteMint matches our token
	filters := []rpc.RPCFilter{
		{
			DataSize: 752, // Raydium V4 pool account size
		},
	}

	// Get all Raydium V4 accounts
	accounts, err := client.GetProgramAccountsWithOpts(
		ctx,
		RAYDIUM_AMM_V4,
		&rpc.GetProgramAccountsOpts{
			Filters: filters,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get program accounts: %w", err)
	}

	fmt.Printf("Found %d Raydium V4 accounts, filtering for token %s...\n", len(accounts), tokenAddress)

	var pools []*OnChainPool
	for _, account := range accounts {
		pool, err := parsePoolAccount(account.Pubkey, account.Account.Data.GetBinary())
		if err != nil {
			continue // Skip invalid pools
		}

		// Check if this pool contains our token paired with SOL/WSOL
		hasOurToken := pool.BaseMint.Equals(tokenPubkey) || pool.QuoteMint.Equals(tokenPubkey)
		hasSol := pool.BaseMint.Equals(WSOL_MINT) || pool.QuoteMint.Equals(WSOL_MINT) ||
			pool.BaseMint.Equals(SOL_MINT) || pool.QuoteMint.Equals(SOL_MINT)

		if hasOurToken && hasSol {
			// Get decimals for the tokens
			pool.BaseDecimals, err = getTokenDecimals(ctx, client, pool.BaseMint.String())
			if err != nil {
				fmt.Printf("Warning: Failed to get base decimals for pool %s: %v\n", account.Pubkey, err)
				continue
			}
			pool.QuoteDecimals, err = getTokenDecimals(ctx, client, pool.QuoteMint.String())
			if err != nil {
				fmt.Printf("Warning: Failed to get quote decimals for pool %s: %v\n", account.Pubkey, err)
				continue
			}

			pools = append(pools, pool)
		}
	}

	if len(pools) == 0 {
		return nil, fmt.Errorf("no pools found for token %s paired with SOL/WSOL", tokenAddress)
	}

	fmt.Printf("Found %d pools for token %s\n", len(pools), tokenAddress)

	// Select pool with highest liquidity (approximated by reserves)
	var bestPool *OnChainPool
	var maxLiquidity uint64

	for _, pool := range pools {
		// Approximate liquidity by the SOL reserves
		var solReserves uint64
		if pool.BaseMint.Equals(WSOL_MINT) || pool.BaseMint.Equals(SOL_MINT) {
			solReserves = pool.BaseAmount
		} else {
			solReserves = pool.QuoteAmount
		}

		if solReserves > maxLiquidity {
			maxLiquidity = solReserves
			bestPool = pool
		}
	}

	return bestPool, nil
}

// parsePoolAccount parses the raw pool account data
func parsePoolAccount(address solana.PublicKey, data []byte) (*OnChainPool, error) {
	if len(data) < 752 {
		return nil, fmt.Errorf("invalid pool data size: %d", len(data))
	}

	pool := &OnChainPool{
		Address: address,
	}

	// Parse mints (offsets based on Raydium V4 structure)
	pool.BaseMint = solana.PublicKeyFromBytes(data[400:432])
	pool.QuoteMint = solana.PublicKeyFromBytes(data[432:464])

	// Parse vaults
	pool.BaseVault = solana.PublicKeyFromBytes(data[496:528])
	pool.QuoteVault = solana.PublicKeyFromBytes(data[528:560])

	// Try multiple offsets for amounts (different pool versions have different structures)
	// First try V4 offsets
	pool.BaseAmount = binary.LittleEndian.Uint64(data[685:693])
	pool.QuoteAmount = binary.LittleEndian.Uint64(data[693:701])
	
	// If values seem wrong (overflow), try alternative offsets
	if pool.BaseAmount > uint64(1e18) || pool.QuoteAmount > uint64(1e18) {
		// Try offsets for older versions
		pool.BaseAmount = binary.LittleEndian.Uint64(data[85:93])
		pool.QuoteAmount = binary.LittleEndian.Uint64(data[93:101])
		
		// If still wrong, try another set
		if pool.BaseAmount > uint64(1e18) || pool.QuoteAmount > uint64(1e18) {
			pool.BaseAmount = binary.LittleEndian.Uint64(data[465:473])
			pool.QuoteAmount = binary.LittleEndian.Uint64(data[473:481])
		}
	}

	return pool, nil
}

// getTokenDecimals fetches the decimals for a token from on-chain
func getTokenDecimals(ctx context.Context, client *rpc.Client, mintAddress string) (uint8, error) {
	// SOL/WSOL always has 9 decimals
	if mintAddress == WSOL_MINT.String() || mintAddress == SOL_MINT.String() {
		return SOL_DECIMALS, nil
	}

	mintPubkey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	// Get mint account info
	accountInfo, err := client.GetAccountInfo(ctx, mintPubkey)
	if err != nil {
		return 0, fmt.Errorf("failed to get mint account: %w", err)
	}

	// Parse mint data
	mintData := accountInfo.Value.Data.GetBinary()
	if len(mintData) < 82 { // Minimum size for SPL Token Mint
		return 0, fmt.Errorf("invalid mint data size")
	}

	// Decimals is at offset 44 in the mint account
	decimals := mintData[44]
	return decimals, nil
}

func calculateQuoteOnChain(ctx context.Context, client *rpc.Client, params QuoteParams) (float64, error) {
	poolPubkey, err := solana.PublicKeyFromBase58(params.PoolAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid pool address: %w", err)
	}

	// Fetch pool account info
	accountInfo, err := client.GetAccountInfo(ctx, poolPubkey)
	if err != nil {
		return 0, fmt.Errorf("failed to get pool account: %w", err)
	}

	// Parse pool data
	pool, err := parsePoolAccount(poolPubkey, accountInfo.Value.Data.GetBinary())
	if err != nil {
		return 0, fmt.Errorf("failed to parse pool data: %w", err)
	}

	// Get decimals
	pool.BaseDecimals, err = getTokenDecimals(ctx, client, pool.BaseMint.String())
	if err != nil {
		return 0, fmt.Errorf("failed to get base decimals: %w", err)
	}
	pool.QuoteDecimals, err = getTokenDecimals(ctx, client, pool.QuoteMint.String())
	if err != nil {
		return 0, fmt.Errorf("failed to get quote decimals: %w", err)
	}

	fmt.Printf("\n=== Pool Information (On-Chain) ===\n")
	fmt.Printf("Pool Address: %s\n", pool.Address)
	fmt.Printf("Base Token: %s (decimals: %d)\n", pool.BaseMint, pool.BaseDecimals)
	fmt.Printf("Quote Token: %s (decimals: %d)\n", pool.QuoteMint, pool.QuoteDecimals)
	fmt.Printf("Base Amount (raw): %d\n", pool.BaseAmount)
	fmt.Printf("Quote Amount (raw): %d\n", pool.QuoteAmount)

	// Calculate reserves in human-readable format
	baseReserve := float64(pool.BaseAmount) / math.Pow(10, float64(pool.BaseDecimals))
	quoteReserve := float64(pool.QuoteAmount) / math.Pow(10, float64(pool.QuoteDecimals))
	fmt.Printf("Base Reserve: %.6f\n", baseReserve)
	fmt.Printf("Quote Reserve: %.6f\n", quoteReserve)

	// Determine swap direction
	var inputDecimals, outputDecimals int
	var isBaseToQuote bool
	
	// Check if base or quote is SOL/WSOL
	isBaseSol := pool.BaseMint.Equals(WSOL_MINT) || pool.BaseMint.Equals(SOL_MINT)
	
	if params.Side == "buy" {
		// Buying: SOL in -> Token out
		inputDecimals = SOL_DECIMALS
		if isBaseSol {
			// SOL is base, token is quote
			outputDecimals = int(pool.QuoteDecimals)
			isBaseToQuote = true
		} else {
			// SOL is quote, token is base
			outputDecimals = int(pool.BaseDecimals)
			isBaseToQuote = false
		}
	} else {
		// Selling: Token in -> SOL out
		outputDecimals = SOL_DECIMALS
		if isBaseSol {
			// SOL is base, token is quote
			inputDecimals = int(pool.QuoteDecimals)
			isBaseToQuote = false
		} else {
			// SOL is quote, token is base
			inputDecimals = int(pool.BaseDecimals)
			isBaseToQuote = true
		}
	}

	// Calculate quote using constant product formula
	amountIn := uint64(params.Amount * math.Pow(10, float64(inputDecimals)))

	var amountOut uint64
	if isBaseToQuote {
		// base -> quote swap
		amountOut = calculateSwapAmount(pool.QuoteAmount, pool.BaseAmount, amountIn)
	} else {
		// quote -> base swap
		amountOut = calculateSwapAmount(pool.BaseAmount, pool.QuoteAmount, amountIn)
	}

	// Apply trading fee (0.25%)
	fee := float64(amountOut) * 0.0025
	amountOutAfterFee := float64(amountOut) - fee

	// Convert back to decimal format
	result := amountOutAfterFee / math.Pow(10, float64(outputDecimals))

	fmt.Printf("\n=== Calculation Details ===\n")
	fmt.Printf("Amount in (raw): %d\n", amountIn)
	fmt.Printf("Amount out (raw): %d\n", amountOut)
	fmt.Printf("Fee (0.25%%): %.0f\n", fee)
	fmt.Printf("Amount out after fee: %.0f\n", amountOutAfterFee)

	return result, nil
}

func calculateSwapAmount(reserveOut, reserveIn, amountIn uint64) uint64 {
	// Constant product AMM formula: x * y = k
	// amountOut = (reserveOut * amountIn) / (reserveIn + amountIn)
	
	numerator := new(big.Int).Mul(
		new(big.Int).SetUint64(reserveOut),
		new(big.Int).SetUint64(amountIn),
	)
	
	denominator := new(big.Int).Add(
		new(big.Int).SetUint64(reserveIn),
		new(big.Int).SetUint64(amountIn),
	)
	
	result := new(big.Int).Div(numerator, denominator)
	
	return result.Uint64()
}