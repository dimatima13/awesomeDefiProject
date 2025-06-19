package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

// Protocol being used
const PROTOCOL = "Raydium V4 AMM (Pure On-Chain)"

// Raydium V4 AMM Program ID
var RAYDIUM_AMM_V4 = solana.MustPublicKeyFromBase58("675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8")

// OpenBook/Serum DEX Program ID
var OPENBOOK_PROGRAM = solana.MustPublicKeyFromBase58("srmqPvymJeFKQ4zGQed1GFppgkRHL9kaELCbyksJtPX")

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

// Raydium swap instruction discriminator
const RAYDIUM_SWAP_INSTRUCTION = uint8(9)

// Raydium authority PDA seed
const AUTHORITY_AMM_SEED = "amm authority"

// SwapInstructionData represents the data for a Raydium V4 swap instruction
type SwapInstructionData struct {
	Instruction   uint8
	AmountIn      uint64
	MinAmountOut  uint64
}

// TransactionReport contains the swap execution details
type TransactionReport struct {
	TxHash         string
	Status         string
	AmountIn       float64
	AmountOut      float64
	ExpectedPrice  float64
	ActualPrice    float64
	Slippage       float64
	ExplorerURL    string
	InputToken     string
	OutputToken    string
}

type QuoteParams struct {
	PoolAddress  string
	TokenAddress string // Alternative to PoolAddress
	Amount       float64
	Side         string // "buy" or "sell"
}

// OnChainPool represents pool data parsed from on-chain
type OnChainPool struct {
	Address         solana.PublicKey
	BaseMint        solana.PublicKey
	QuoteMint       solana.PublicKey
	BaseVault       solana.PublicKey
	QuoteVault      solana.PublicKey
	BaseAmount      uint64
	QuoteAmount     uint64
	BaseDecimals    uint8
	QuoteDecimals   uint8
	// Additional fields for swap instruction
	Authority       solana.PublicKey
	OpenOrders      solana.PublicKey
	TargetOrders    solana.PublicKey
	MarketProgram   solana.PublicKey
	Market          solana.PublicKey
	MarketBids      solana.PublicKey
	MarketAsks      solana.PublicKey
	MarketEventQueue solana.PublicKey
	MarketBaseVault  solana.PublicKey
	MarketQuoteVault solana.PublicKey
	Nonce           uint8
	MarketNonce     uint8
}

// loadWallet loads a wallet from the SOLANA_PRIVATE_KEY environment variable
func loadWallet() (solana.PrivateKey, error) {
	privateKeyStr := os.Getenv("SOLANA_PRIVATE_KEY")
	if privateKeyStr == "" {
		return nil, fmt.Errorf("SOLANA_PRIVATE_KEY environment variable not set")
	}

	privateKey, err := solana.PrivateKeyFromBase58(privateKeyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %w", err)
	}

	return privateKey, nil
}

// confirmQuote asks the user to confirm the quote before execution
func confirmQuote(poolAddress string, side string, amountIn float64, expectedOut float64) bool {
	scanner := bufio.NewScanner(os.Stdin)
	
	// Calculate price
	var price float64
	if side == "buy" {
		price = amountIn / expectedOut // SOL per token
	} else {
		price = expectedOut / amountIn // SOL per token
	}
	
	fmt.Printf("\n=== SWAP CONFIRMATION ===\n")
	fmt.Printf("Pool: %s\n", poolAddress)
	fmt.Printf("Operation: %s\n", strings.ToUpper(side))
	fmt.Printf("Amount In: %.9f %s\n", amountIn, getInputToken(side))
	fmt.Printf("Expected Out: %.9f %s\n", expectedOut, getOutputToken(side))
	fmt.Printf("Price: %.9f SOL per token\n", price)
	fmt.Printf("========================\n\n")
	
	fmt.Print("Do you want to execute this swap? (y/n): ")
	if !scanner.Scan() {
		fmt.Println("\nSwap cancelled.")
		return false
	}
	
	response := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return response == "y" || response == "yes"
}

// Helper functions to get token names based on side
func getInputToken(side string) string {
	if side == "buy" {
		return "SOL"
	}
	return "TOKEN"
}

func getOutputToken(side string) string {
	if side == "buy" {
		return "TOKEN"
	}
	return "SOL"
}

// getSlippageFromUser asks the user for maximum slippage tolerance
func getSlippageFromUser() (float64, error) {
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Print("\nEnter maximum slippage tolerance (%) [default: 0.5]: ")
	if !scanner.Scan() {
		return 0.5, nil // Use default
	}
	
	input := strings.TrimSpace(scanner.Text())
	
	// Use default if empty
	if input == "" {
		return 0.5, nil
	}
	
	// Parse the input
	slippage, err := parseFloat(input)
	if err != nil {
		return 0, fmt.Errorf("invalid slippage value: %w", err)
	}
	
	// Validate range
	if slippage < 0 || slippage > 100 {
		return 0, fmt.Errorf("slippage must be between 0 and 100")
	}
	
	return slippage, nil
}

// parseFloat parses a string to float64, handling both decimal and percentage formats
func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	return strconv.ParseFloat(s, 64)
}

// calculateMinAmountOut calculates minimum amount out based on slippage
func calculateMinAmountOut(expectedOut float64, slippagePercent float64, outputDecimals int) uint64 {
	// Calculate minimum amount with slippage
	minAmount := expectedOut * (1 - slippagePercent/100)
	
	// Convert to raw amount
	minAmountRaw := uint64(minAmount * math.Pow(10, float64(outputDecimals)))
	
	return minAmountRaw
}

// getOrCreateATA gets or creates an Associated Token Account
func getOrCreateATA(
	ctx context.Context,
	client *rpc.Client,
	wallet solana.PublicKey,
	mint solana.PublicKey,
) (solana.PublicKey, solana.Instruction, error) {
	ata, _, err := solana.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return solana.PublicKey{}, nil, fmt.Errorf("failed to find ATA: %w", err)
	}

	// Check if ATA exists
	accountInfo, err := client.GetAccountInfo(ctx, ata)
	if err != nil || accountInfo == nil || accountInfo.Value == nil {
		// ATA doesn't exist, create instruction to create it
		createATAIx := associatedtokenaccount.NewCreateInstruction(
			wallet,
			wallet,
			mint,
		).Build()
		return ata, createATAIx, nil
	}

	// ATA exists
	return ata, nil, nil
}

// createSwapInstruction creates a Raydium V4 swap instruction
func createSwapInstruction(
	pool *OnChainPool,
	userSource solana.PublicKey,
	userDestination solana.PublicKey,
	userOwner solana.PublicKey,
	amountIn uint64,
	minAmountOut uint64,
	isBaseToQuote bool,
) (solana.Instruction, error) {
	// Serialize instruction data using little-endian encoding
	buf := new(bytes.Buffer)
	
	// Write instruction type (1 byte)
	buf.WriteByte(RAYDIUM_SWAP_INSTRUCTION)
	
	// Write amountIn (8 bytes, little-endian)
	amountInBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(amountInBytes, amountIn)
	buf.Write(amountInBytes)
	
	// Write minAmountOut (8 bytes, little-endian)
	minAmountOutBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(minAmountOutBytes, minAmountOut)
	buf.Write(minAmountOutBytes)

	// Get market vault signer PDA if we have a real market
	var marketVaultSigner solana.PublicKey
	if !pool.Market.IsZero() && pool.MarketProgram.String() == OPENBOOK_PROGRAM.String() {
		// For OpenBook/Serum markets, the vault signer is derived differently
		// We need to find the correct nonce that produces a valid PDA
		var err error
		var found bool
		for nonce := uint8(0); nonce < 255; nonce++ {
			candidate, err := solana.CreateProgramAddress(
				[][]byte{
					pool.Market.Bytes(),
					{nonce},
				},
				pool.MarketProgram,
			)
			if err == nil {
				marketVaultSigner = candidate
				found = true
				break
			}
		}
		if !found {
			// If we can't find it, use the pool's stored nonce
			marketVaultSigner, _, err = solana.FindProgramAddress(
				[][]byte{
					pool.Market.Bytes(),
				},
				pool.MarketProgram,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to find market vault signer PDA: %w", err)
		}
	} else {
		// Use a dummy account if no real market
		marketVaultSigner = solana.SystemProgramID
	}

	// Build full account list for Raydium V4 swap
	accounts := []*solana.AccountMeta{
		// 0. Token program
		{PublicKey: token.ProgramID, IsSigner: false, IsWritable: false},
		// 1. AMM pool
		{PublicKey: pool.Address, IsSigner: false, IsWritable: true},
		// 2. AMM authority
		{PublicKey: pool.Authority, IsSigner: false, IsWritable: false},
		// 3. AMM open orders
		{PublicKey: pool.OpenOrders, IsSigner: false, IsWritable: true},
		// 4. AMM target orders
		{PublicKey: pool.TargetOrders, IsSigner: false, IsWritable: true},
		// 5. Pool base vault
		{PublicKey: pool.BaseVault, IsSigner: false, IsWritable: true},
		// 6. Pool quote vault
		{PublicKey: pool.QuoteVault, IsSigner: false, IsWritable: true},
		// 7. Market program
		{PublicKey: pool.MarketProgram, IsSigner: false, IsWritable: false},
		// 8. Market
		{PublicKey: pool.Market, IsSigner: false, IsWritable: true},
		// 9. Market bids
		{PublicKey: pool.MarketBids, IsSigner: false, IsWritable: true},
		// 10. Market asks
		{PublicKey: pool.MarketAsks, IsSigner: false, IsWritable: true},
		// 11. Market event queue
		{PublicKey: pool.MarketEventQueue, IsSigner: false, IsWritable: true},
		// 12. Market base vault
		{PublicKey: pool.MarketBaseVault, IsSigner: false, IsWritable: true},
		// 13. Market quote vault
		{PublicKey: pool.MarketQuoteVault, IsSigner: false, IsWritable: true},
		// 14. Market vault signer
		{PublicKey: marketVaultSigner, IsSigner: false, IsWritable: false},
		// 15. User source token account
		{PublicKey: userSource, IsSigner: false, IsWritable: true},
		// 16. User destination token account
		{PublicKey: userDestination, IsSigner: false, IsWritable: true},
		// 17. User owner (signer)
		{PublicKey: userOwner, IsSigner: true, IsWritable: false},
	}
	
	fmt.Printf("\n=== DEBUG - Swap Instruction Accounts ===\n")
	for i, acc := range accounts {
		writable := "R"
		if acc.IsWritable {
			writable = "W"
		}
		signer := ""
		if acc.IsSigner {
			signer = " [SIGNER]"
		}
		fmt.Printf("%2d. %s (%s)%s\n", i, acc.PublicKey, writable, signer)
	}
	fmt.Printf("=========================================\n")
	
	fmt.Printf("\n=== DEBUG - Swap Parameters ===\n")
	fmt.Printf("AmountIn: %d\n", amountIn)
	fmt.Printf("MinAmountOut: %d\n", minAmountOut)
	fmt.Printf("IsBaseToQuote: %v\n", isBaseToQuote)
	fmt.Printf("MarketVaultSigner: %s\n", marketVaultSigner)
	fmt.Printf("Instruction data (hex): %x\n", buf.Bytes())
	fmt.Printf("Instruction data length: %d bytes\n", buf.Len())
	fmt.Printf("================================\n")

	instruction := solana.NewInstruction(
		RAYDIUM_AMM_V4,
		accounts,
		buf.Bytes(),
	)

	return instruction, nil
}

// executeSwap builds and executes the swap transaction
func executeSwap(
	ctx context.Context,
	client *rpc.Client,
	wallet solana.PrivateKey,
	poolAddress string,
	side string,
	amountIn float64,
	minAmountOut uint64,
) (string, error) {
	poolPubkey, err := solana.PublicKeyFromBase58(poolAddress)
	if err != nil {
		return "", fmt.Errorf("invalid pool address: %w", err)
	}

	// Fetch pool data
	accountInfo, err := client.GetAccountInfo(ctx, poolPubkey)
	if err != nil {
		return "", fmt.Errorf("failed to get pool account: %w", err)
	}

	pool, err := parsePoolAccount(poolPubkey, accountInfo.Value.Data.GetBinary())
	if err != nil {
		return "", fmt.Errorf("failed to parse pool data: %w", err)
	}

	// Debug mints
	fmt.Printf("\n=== DEBUG - Token mints ===\n")
	fmt.Printf("BaseMint: %s\n", pool.BaseMint)
	fmt.Printf("QuoteMint: %s\n", pool.QuoteMint)
	
	// Get decimals
	pool.BaseDecimals, err = getTokenDecimals(ctx, client, pool.BaseMint.String())
	if err != nil {
		return "", fmt.Errorf("failed to get base decimals for %s: %w", pool.BaseMint, err)
	}
	pool.QuoteDecimals, err = getTokenDecimals(ctx, client, pool.QuoteMint.String())
	if err != nil {
		return "", fmt.Errorf("failed to get quote decimals for %s: %w", pool.QuoteMint, err)
	}

	// Fetch actual vault balances
	err = fetchVaultBalances(ctx, client, pool)
	if err != nil {
		return "", fmt.Errorf("failed to fetch vault balances: %w", err)
	}

	// Fetch market data for the pool
	err = fetchMarketData(ctx, client, pool)
	if err != nil {
		// If we can't fetch market data, use fallback values
		fmt.Printf("Warning: Failed to fetch market data: %v\n", err)
		fmt.Println("Using fallback values for market accounts...")
		pool.MarketBaseVault = pool.BaseVault
		pool.MarketQuoteVault = pool.QuoteVault
		pool.MarketBids = solana.SystemProgramID
		pool.MarketAsks = solana.SystemProgramID
		pool.MarketEventQueue = solana.SystemProgramID
	}
	
	// Debug info
	fmt.Printf("\n=== DEBUG - Pool accounts ===\n")
	fmt.Printf("Pool: %s\n", pool.Address)
	fmt.Printf("Authority: %s\n", pool.Authority)
	fmt.Printf("OpenOrders: %s\n", pool.OpenOrders)
	fmt.Printf("TargetOrders: %s\n", pool.TargetOrders)
	fmt.Printf("BaseVault: %s\n", pool.BaseVault)
	fmt.Printf("QuoteVault: %s\n", pool.QuoteVault)
	fmt.Printf("BaseMint: %s\n", pool.BaseMint)
	fmt.Printf("QuoteMint: %s\n", pool.QuoteMint)
	fmt.Printf("Market: %s\n", pool.Market)
	fmt.Printf("MarketProgram: %s\n", pool.MarketProgram)
	fmt.Printf("MarketBids: %s\n", pool.MarketBids)
	fmt.Printf("MarketAsks: %s\n", pool.MarketAsks)
	fmt.Printf("MarketEventQueue: %s\n", pool.MarketEventQueue)
	fmt.Printf("MarketBaseVault: %s\n", pool.MarketBaseVault)
	fmt.Printf("MarketQuoteVault: %s\n", pool.MarketQuoteVault)
	fmt.Printf("Nonce: %d\n", pool.Nonce)
	fmt.Printf("=============================\n")
	

	// Determine swap direction and mints
	var (
		sourceMint      solana.PublicKey
		destinationMint solana.PublicKey
		isBaseToQuote   bool
		inputDecimals   int
	)

	isBaseSol := pool.BaseMint.Equals(WSOL_MINT) || pool.BaseMint.Equals(SOL_MINT)

	if side == "buy" {
		// Buying: SOL -> Token
		inputDecimals = SOL_DECIMALS
		if isBaseSol {
			sourceMint = pool.BaseMint
			destinationMint = pool.QuoteMint
			isBaseToQuote = true
		} else {
			sourceMint = pool.QuoteMint
			destinationMint = pool.BaseMint
			isBaseToQuote = false
		}
	} else {
		// Selling: Token -> SOL
		if isBaseSol {
			sourceMint = pool.QuoteMint
			destinationMint = pool.BaseMint
			inputDecimals = int(pool.QuoteDecimals)
			isBaseToQuote = false
		} else {
			sourceMint = pool.BaseMint
			destinationMint = pool.QuoteMint
			inputDecimals = int(pool.BaseDecimals)
			isBaseToQuote = true
		}
	}

	// Convert amount to raw
	amountInRaw := uint64(amountIn * math.Pow(10, float64(inputDecimals)))

	// Get or create ATAs
	instructions := []solana.Instruction{}
	signers := []solana.PrivateKey{wallet}
	
	// Skip compute budget for now to simplify debugging

	// Source ATA
	sourceATA, createSourceIx, err := getOrCreateATA(ctx, client, wallet.PublicKey(), sourceMint)
	if err != nil {
		return "", fmt.Errorf("failed to get source ATA: %w", err)
	}
	if createSourceIx != nil {
		fmt.Printf("Creating source ATA for mint %s\n", sourceMint)
		instructions = append(instructions, createSourceIx)
	}

	// For WSOL, we need to create a wrapped SOL account and transfer SOL
	if sourceMint.Equals(WSOL_MINT) && side == "buy" {
		fmt.Printf("Wrapping SOL: transferring %d lamports to WSOL ATA %s\n", amountInRaw, sourceATA)
		// Transfer SOL to the WSOL ATA
		transferIx := system.NewTransferInstruction(
			amountInRaw,
			wallet.PublicKey(),
			sourceATA,
		).Build()
		instructions = append(instructions, transferIx)

		// Sync native to update the WSOL balance
		syncIx := token.NewSyncNativeInstruction(sourceATA).Build()
		instructions = append(instructions, syncIx)
	}

	// Destination ATA
	destinationATA, createDestIx, err := getOrCreateATA(ctx, client, wallet.PublicKey(), destinationMint)
	if err != nil {
		return "", fmt.Errorf("failed to get destination ATA: %w", err)
	}
	if createDestIx != nil {
		fmt.Printf("Creating destination ATA for mint %s\n", destinationMint)
		instructions = append(instructions, createDestIx)
	}
	
	fmt.Printf("\n=== DEBUG - Token Accounts ===\n")
	fmt.Printf("Source mint: %s\n", sourceMint)
	fmt.Printf("Source ATA: %s\n", sourceATA)
	fmt.Printf("Destination mint: %s\n", destinationMint)
	fmt.Printf("Destination ATA: %s\n", destinationATA)
	fmt.Printf("==============================\n")

	// Create swap instruction
	swapIx, err := createSwapInstruction(
		pool,
		sourceATA,
		destinationATA,
		wallet.PublicKey(),
		amountInRaw,
		minAmountOut,
		isBaseToQuote,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create swap instruction: %w", err)
	}
	instructions = append(instructions, swapIx)

	// For WSOL output, close the account to unwrap
	if destinationMint.Equals(WSOL_MINT) && side == "sell" {
		closeIx := token.NewCloseAccountInstruction(
			destinationATA,
			wallet.PublicKey(),
			wallet.PublicKey(),
			[]solana.PublicKey{},
		).Build()
		instructions = append(instructions, closeIx)
	}

	// Get latest blockhash
	latestBlockhash, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		instructions,
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(wallet.PublicKey()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}
	
	// Debug transaction info
	fmt.Printf("\n=== DEBUG - Transaction Info ===\n")
	fmt.Printf("Instructions count: %d\n", len(instructions))
	fmt.Printf("Blockhash: %s\n", latestBlockhash.Value.Blockhash)
	fmt.Printf("Fee payer: %s\n", wallet.PublicKey())
	fmt.Printf("Signers: %d\n", len(signers))
	for i, ix := range instructions {
		fmt.Printf("Instruction %d: Program %s\n", i, ix.ProgramID())
	}
	fmt.Printf("================================\n")

	// Sign transaction
	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			for _, signer := range signers {
				if signer.PublicKey().Equals(key) {
					return &signer
				}
			}
			return nil
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send transaction with more detailed error handling
	fmt.Println("\nSending transaction...")
	
	// First try with preflight to get better error messages
	sig, err := client.SendTransactionWithOpts(
		ctx,
		tx,
		rpc.TransactionOpts{
			SkipPreflight:       false,
			PreflightCommitment: rpc.CommitmentFinalized,
		},
	)
	
	if err != nil {
		// If preflight fails, try without it to get the actual on-chain error
		if strings.Contains(err.Error(), "Transaction signature verification failure") {
			fmt.Println("Preflight failed, trying without preflight to get on-chain error...")
			sig, err = client.SendTransactionWithOpts(
				ctx,
				tx,
				rpc.TransactionOpts{
					SkipPreflight:       true,
					PreflightCommitment: rpc.CommitmentFinalized,
				},
			)
			if err != nil {
				return "", fmt.Errorf("failed to send transaction: %w", err)
			}
		} else {
			return "", fmt.Errorf("failed to send transaction: %w", err)
		}
	}

	// Wait for confirmation
	fmt.Println("Waiting for confirmation...")
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		time.Sleep(1 * time.Second)
		
		status, err := client.GetSignatureStatuses(ctx, false, sig)
		if err != nil {
			continue
		}
		
		if status != nil && len(status.Value) > 0 && status.Value[0] != nil {
			if status.Value[0].ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
			   status.Value[0].ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				break
			}
		}
	}

	return sig.String(), nil
}

// parseSwapResult fetches transaction details and extracts swap amounts
func parseSwapResult(
	ctx context.Context,
	client *rpc.Client,
	txHash string,
	wallet solana.PublicKey,
) (actualIn float64, actualOut float64, err error) {
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid transaction hash: %w", err)
	}

	// Get transaction details
	tx, err := client.GetTransaction(
		ctx,
		sig,
		&rpc.GetTransactionOpts{
			Encoding:   solana.EncodingBase64,
			Commitment: rpc.CommitmentConfirmed,
		},
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get transaction: %w", err)
	}

	if tx == nil || tx.Meta == nil {
		return 0, 0, fmt.Errorf("transaction not found or no metadata")
	}

	// Check if transaction was successful
	if tx.Meta.Err != nil {
		return 0, 0, fmt.Errorf("transaction failed: %v", tx.Meta.Err)
	}

	// For simplicity, we'll use the pre/post token balances
	// In a real implementation, you would parse the logs for exact swap amounts
	preBalances := tx.Meta.PreTokenBalances
	postBalances := tx.Meta.PostTokenBalances

	// Calculate the differences
	// This is a simplified version - in production you'd need more robust parsing
	var tokenIn, tokenOut float64

	// Find balance changes for the wallet
	for _, preBalance := range preBalances {
		if preBalance.Owner != nil && preBalance.Owner.String() == wallet.String() {
			// Find corresponding post balance
			for _, postBalance := range postBalances {
				if postBalance.Owner != nil && postBalance.Owner.String() == wallet.String() &&
					preBalance.Mint == postBalance.Mint {
					
					preAmount, _ := strconv.ParseFloat(preBalance.UiTokenAmount.UiAmountString, 64)
					postAmount, _ := strconv.ParseFloat(postBalance.UiTokenAmount.UiAmountString, 64)
					
					diff := postAmount - preAmount
					if diff < 0 {
						tokenIn = -diff // Amount that left the wallet
					} else if diff > 0 {
						tokenOut = diff // Amount that entered the wallet
					}
					break
				}
			}
		}
	}

	// If we couldn't find token balances, try SOL balance changes
	if tokenIn == 0 && tokenOut == 0 && tx.Meta.PreBalances != nil && tx.Meta.PostBalances != nil {
		// For now, we'll use the token balance changes as the primary source
		// SOL balance parsing would require decoding the transaction which is complex
		if len(preBalances) == 0 {
			// Fallback values if we can't parse
			return 0, 0, fmt.Errorf("could not parse transaction balances")
		}
	}

	return tokenIn, tokenOut, nil
}

// generateReport creates a detailed transaction report
func generateReport(
	ctx context.Context,
	client *rpc.Client,
	wallet solana.PublicKey,
	txHash string,
	side string,
	expectedIn float64,
	expectedOut float64,
	slippageTolerance float64,
) (*TransactionReport, error) {
	// Parse transaction to get actual amounts
	actualIn, actualOut, err := parseSwapResult(ctx, client, txHash, wallet)
	if err != nil {
		// If we can't parse, use expected values
		actualIn = expectedIn
		actualOut = expectedOut
	}

	// Calculate prices
	var expectedPrice, actualPrice float64
	if side == "buy" {
		expectedPrice = expectedIn / expectedOut   // SOL per token
		if actualOut > 0 {
			actualPrice = actualIn / actualOut
		} else {
			actualPrice = expectedPrice
		}
	} else {
		expectedPrice = expectedOut / expectedIn   // SOL per token
		if actualIn > 0 {
			actualPrice = actualOut / actualIn
		} else {
			actualPrice = expectedPrice
		}
	}

	// Calculate actual slippage
	var slippage float64
	if expectedPrice > 0 {
		slippage = math.Abs((actualPrice-expectedPrice)/expectedPrice) * 100
	}

	report := &TransactionReport{
		TxHash:        txHash,
		Status:        "Success",
		AmountIn:      actualIn,
		AmountOut:     actualOut,
		ExpectedPrice: expectedPrice,
		ActualPrice:   actualPrice,
		Slippage:      slippage,
		ExplorerURL:   fmt.Sprintf("https://solscan.io/tx/%s", txHash),
		InputToken:    getInputToken(side),
		OutputToken:   getOutputToken(side),
	}

	return report, nil
}

// printReport displays the transaction report
func printReport(report *TransactionReport) {
	fmt.Printf("\n=== TRANSACTION REPORT ===\n")
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Transaction: %s\n", report.TxHash)
	fmt.Printf("Explorer: %s\n", report.ExplorerURL)
	fmt.Printf("\nSwap Details:\n")
	fmt.Printf("  Amount In: %.9f %s\n", report.AmountIn, report.InputToken)
	fmt.Printf("  Amount Out: %.9f %s\n", report.AmountOut, report.OutputToken)
	fmt.Printf("\nPrice Analysis:\n")
	fmt.Printf("  Expected Price: %.9f SOL per token\n", report.ExpectedPrice)
	fmt.Printf("  Actual Price: %.9f SOL per token\n", report.ActualPrice)
	fmt.Printf("  Price Impact: %.4f%%\n", report.Slippage)
	fmt.Printf("========================\n")
}

func main() {
	var poolAddr string
	var tokenAddr string
	var amount float64
	var side string
	var execute bool

	flag.StringVar(&poolAddr, "pool", "", "Pool address")
	flag.StringVar(&tokenAddr, "token", "", "Token address (finds best pool)")
	flag.Float64Var(&amount, "amount", 0, "Amount to swap")
	flag.StringVar(&side, "side", "", "buy or sell")
	flag.BoolVar(&execute, "execute", false, "Execute the swap (requires SOLANA_PRIVATE_KEY)")
	flag.Parse()

	if amount == 0 || side == "" {
		fmt.Println("Usage: go run main.go [-pool POOL | -token TOKEN] -amount AMOUNT -side buy|sell [-execute]")
		flag.PrintDefaults()
		return
	}

	if poolAddr == "" && tokenAddr == "" {
		log.Fatal("Either -pool or -token must be specified")
	}

	if side != "buy" && side != "sell" {
		log.Fatal("Side must be 'buy' or 'sell'")
	}

	// Load wallet if execute flag is set
	var wallet solana.PrivateKey
	if execute {
		var err error
		wallet, err = loadWallet()
		if err != nil {
			log.Fatalf("Failed to load wallet: %v", err)
		}
		fmt.Printf("Wallet loaded: %s\n", wallet.PublicKey())
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

	// If execute flag is set, proceed with swap execution
	if execute {
		// Confirm the quote with the user
		if !confirmQuote(poolAddress, side, amount, quote) {
			fmt.Println("\nSwap cancelled by user.")
			return
		}
		
		// Get slippage tolerance
		slippage, err := getSlippageFromUser()
		if err != nil {
			log.Fatalf("Failed to get slippage: %v", err)
		}
		
		// Get pool data to determine correct output decimals
		poolPubkey, _ := solana.PublicKeyFromBase58(poolAddress)
		accountInfo, err := client.GetAccountInfo(ctx, poolPubkey)
		if err != nil {
			log.Fatalf("Failed to get pool account: %v", err)
		}
		
		pool, err := parsePoolAccount(poolPubkey, accountInfo.Value.Data.GetBinary())
		if err != nil {
			log.Fatalf("Failed to parse pool data: %v", err)
		}
		
		// Get decimals
		pool.BaseDecimals, _ = getTokenDecimals(ctx, client, pool.BaseMint.String())
		pool.QuoteDecimals, _ = getTokenDecimals(ctx, client, pool.QuoteMint.String())
		
		// Fetch actual vault balances
		_ = fetchVaultBalances(ctx, client, pool)
		
		// Calculate minimum amount out with correct decimals
		var outputDecimals int
		isBaseSol := pool.BaseMint.Equals(WSOL_MINT) || pool.BaseMint.Equals(SOL_MINT)
		
		if side == "buy" {
			// Buying token, output is token
			if isBaseSol {
				outputDecimals = int(pool.QuoteDecimals)
			} else {
				outputDecimals = int(pool.BaseDecimals)
			}
		} else {
			// Selling token, output is SOL
			outputDecimals = SOL_DECIMALS
		}
		
		minAmountOut := calculateMinAmountOut(quote, slippage, outputDecimals)
		
		fmt.Printf("\n=== SWAP PARAMETERS ===\n")
		fmt.Printf("Slippage Tolerance: %.2f%%\n", slippage)
		fmt.Printf("Expected Out: %.9f\n", quote)
		fmt.Printf("Minimum Out: %.9f\n", float64(minAmountOut)/math.Pow(10, float64(outputDecimals)))
		fmt.Printf("======================\n")
		
		// Execute the swap
		txHash, err := executeSwap(ctx, client, wallet, poolAddress, side, amount, minAmountOut)
		if err != nil {
			log.Fatalf("Swap failed: %v", err)
		}
		
		fmt.Printf("\n✅ Swap executed successfully!\n")
		fmt.Printf("Transaction: %s\n", txHash)
		
		// Wait a moment for transaction to be fully confirmed
		fmt.Println("\nFetching transaction details...")
		time.Sleep(2 * time.Second)
		
		// Generate and display transaction report
		report, err := generateReport(ctx, client, wallet.PublicKey(), txHash, side, amount, quote, slippage)
		if err != nil {
			fmt.Printf("Warning: Could not generate full report: %v\n", err)
			fmt.Printf("Explorer: https://solscan.io/tx/%s\n", txHash)
		} else {
			printReport(report)
		}
	}
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

			// Fetch actual vault balances
			err = fetchVaultBalances(ctx, client, pool)
			if err != nil {
				fmt.Printf("Warning: Failed to fetch vault balances for pool %s: %v\n", account.Pubkey, err)
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

	// Raydium V4 AMM pool layout - verified working offsets
	
	// offset 8: nonce (1 byte within status u64)
	pool.Nonce = data[8]
	
	// PublicKey fields start at offset 336
	pool.BaseVault = solana.PublicKeyFromBytes(data[336:368])       // coin_vault
	pool.QuoteVault = solana.PublicKeyFromBytes(data[368:400])      // pc_vault  
	pool.BaseMint = solana.PublicKeyFromBytes(data[400:432])        // coin_mint
	pool.QuoteMint = solana.PublicKeyFromBytes(data[432:464])       // pc_mint
	pool.OpenOrders = solana.PublicKeyFromBytes(data[464:496])      // open_orders
	pool.TargetOrders = solana.PublicKeyFromBytes(data[592:624])    // target_orders
	pool.Market = solana.PublicKeyFromBytes(data[656:688])          // market
	pool.MarketProgram = solana.PublicKeyFromBytes(data[688:720])   // market_program
	
	// Get pool amounts - these need to be fetched from vault accounts
	// Initialize to 0, will be populated by fetchVaultBalances
	pool.BaseAmount = 0
	pool.QuoteAmount = 0
	
	// Calculate authority PDA
	// According to Raydium source code, authority is derived using only "amm authority" seed
	authority, nonce, err := solana.FindProgramAddress(
		[][]byte{
			[]byte(AUTHORITY_AMM_SEED),
		},
		RAYDIUM_AMM_V4,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to derive authority PDA: %w", err)
	}
	
	// Verify the nonce matches what's stored in the pool data
	if nonce != pool.Nonce {
		fmt.Printf("Warning: Authority nonce mismatch. Expected: %d, Got: %d\n", pool.Nonce, nonce)
	}
	
	pool.Authority = authority

	return pool, nil
}

// fetchMarketData fetches the OpenBook/Serum market data
func fetchMarketData(ctx context.Context, client *rpc.Client, pool *OnChainPool) error {
	// Check if market is zero (some pools don't have external markets)
	if pool.Market.IsZero() {
		// Use pool vaults as market vaults for pools without external market
		pool.MarketBaseVault = pool.BaseVault
		pool.MarketQuoteVault = pool.QuoteVault
		// Create dummy accounts for other market fields
		pool.MarketBids = solana.SystemProgramID
		pool.MarketAsks = solana.SystemProgramID
		pool.MarketEventQueue = solana.SystemProgramID
		pool.MarketNonce = 0
		return nil
	}

	// Get market account info
	marketInfo, err := client.GetAccountInfo(ctx, pool.Market)
	if err != nil {
		return fmt.Errorf("failed to get market account: %w", err)
	}

	marketData := marketInfo.Value.Data.GetBinary()
	if len(marketData) < 388 { // Minimum size for OpenBook market
		// This might be a different type of market or invalid
		// Use pool vaults as fallback
		pool.MarketBaseVault = pool.BaseVault
		pool.MarketQuoteVault = pool.QuoteVault
		pool.MarketBids = solana.SystemProgramID
		pool.MarketAsks = solana.SystemProgramID
		pool.MarketEventQueue = solana.SystemProgramID
		pool.MarketNonce = 0
		return nil
	}

	// Parse market data (OpenBook/Serum V3 layout)
	pool.MarketBaseVault = solana.PublicKeyFromBytes(marketData[84:116])   // Base vault
	pool.MarketQuoteVault = solana.PublicKeyFromBytes(marketData[116:148]) // Quote vault
	pool.MarketBids = solana.PublicKeyFromBytes(marketData[316:348])       // Bids
	pool.MarketAsks = solana.PublicKeyFromBytes(marketData[348:380])       // Asks
	pool.MarketEventQueue = solana.PublicKeyFromBytes(marketData[252:284]) // Event queue
	
	// Get vault signer nonce (at offset 45 in Serum V3)
	if len(marketData) > 45 {
		pool.MarketNonce = marketData[45]
	}

	return nil
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

// fetchVaultBalances fetches the actual token balances from vault accounts
func fetchVaultBalances(ctx context.Context, client *rpc.Client, pool *OnChainPool) error {
	// Get base vault balance
	baseVaultInfo, err := client.GetTokenAccountBalance(ctx, pool.BaseVault, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get base vault balance: %w", err)
	}
	
	// Get quote vault balance
	quoteVaultInfo, err := client.GetTokenAccountBalance(ctx, pool.QuoteVault, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get quote vault balance: %w", err)
	}
	
	// Parse amounts
	pool.BaseAmount, err = strconv.ParseUint(baseVaultInfo.Value.Amount, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse base amount: %w", err)
	}
	
	pool.QuoteAmount, err = strconv.ParseUint(quoteVaultInfo.Value.Amount, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse quote amount: %w", err)
	}
	
	return nil
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

	// Debug mints
	fmt.Printf("\n=== DEBUG - Token mints (calculateQuote) ===\n")
	fmt.Printf("BaseMint: %s\n", pool.BaseMint)
	fmt.Printf("QuoteMint: %s\n", pool.QuoteMint)
	
	// Get decimals
	pool.BaseDecimals, err = getTokenDecimals(ctx, client, pool.BaseMint.String())
	if err != nil {
		return 0, fmt.Errorf("failed to get base decimals: %w", err)
	}
	pool.QuoteDecimals, err = getTokenDecimals(ctx, client, pool.QuoteMint.String())
	if err != nil {
		return 0, fmt.Errorf("failed to get quote decimals: %w", err)
	}

	// Fetch actual vault balances
	err = fetchVaultBalances(ctx, client, pool)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch vault balances: %w", err)
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