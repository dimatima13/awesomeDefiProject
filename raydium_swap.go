package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

// RaydiumSwapLayout represents the instruction data for Raydium swap
type RaydiumSwapLayout struct {
	Instruction   uint8  `bin:"required"`
	AmountIn      uint64 `bin:"required"`
	MinAmountOut  uint64 `bin:"required"`
}

// RaydiumPoolInfo from API
type RaydiumPoolInfo struct {
	ID              string `json:"id"`
	BaseMint        string `json:"baseMint"`
	QuoteMint       string `json:"quoteMint"`
	LpMint          string `json:"lpMint"`
	BaseDecimals    int    `json:"baseDecimals"`
	QuoteDecimals   int    `json:"quoteDecimals"`
	LpDecimals      int    `json:"lpDecimals"`
	Version         int    `json:"version"`
	ProgramID       string `json:"programId"`
	Authority       string `json:"authority"`
	OpenOrders      string `json:"openOrders"`
	TargetOrders    string `json:"targetOrders"`
	BaseVault       string `json:"baseVault"`
	QuoteVault      string `json:"quoteVault"`
	WithdrawQueue   string `json:"withdrawQueue"`
	LpVault         string `json:"lpVault"`
	MarketVersion   int    `json:"marketVersion"`
	MarketProgramID string `json:"marketProgramId"`
	MarketID        string `json:"marketId"`
	MarketAuthority string `json:"marketAuthority"`
	MarketBaseVault string `json:"marketBaseVault"`
	MarketQuoteVault string `json:"marketQuoteVault"`
	MarketBids      string `json:"marketBids"`
	MarketAsks      string `json:"marketAsks"`
	MarketEventQueue string `json:"marketEventQueue"`
}

// GetRaydiumPoolInfo fetches pool info from Raydium API
func GetRaydiumPoolInfo(poolID string) (*RaydiumPoolInfo, error) {
	url := fmt.Sprintf("https://api.raydium.io/v2/sdk/liquidity/mainnet/%s", poolID)
	
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pool info: %w", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	var pool RaydiumPoolInfo
	if err := json.Unmarshal(body, &pool); err != nil {
		return nil, fmt.Errorf("failed to parse pool info: %w", err)
	}
	
	return &pool, nil
}

// CreateRaydiumSwapInstruction creates a swap instruction for Raydium
func CreateRaydiumSwapInstruction(
	pool *RaydiumPoolInfo,
	userSourceTokenAccount solana.PublicKey,
	userDestTokenAccount solana.PublicKey,
	userOwner solana.PublicKey,
	amountIn uint64,
	minAmountOut uint64,
) (solana.Instruction, error) {
	// Encode instruction data
	data := RaydiumSwapLayout{
		Instruction:  9, // Swap instruction
		AmountIn:     amountIn,
		MinAmountOut: minAmountOut,
	}
	
	buf := new(bytes.Buffer)
	if err := bin.NewBorshEncoder(buf).Encode(data); err != nil {
		return nil, fmt.Errorf("failed to encode instruction: %w", err)
	}
	
	// Convert string addresses to PublicKey
	poolPubkey := solana.MustPublicKeyFromBase58(pool.ID)
	authority := solana.MustPublicKeyFromBase58(pool.Authority)
	openOrders := solana.MustPublicKeyFromBase58(pool.OpenOrders)
	targetOrders := solana.MustPublicKeyFromBase58(pool.TargetOrders)
	baseVault := solana.MustPublicKeyFromBase58(pool.BaseVault)
	quoteVault := solana.MustPublicKeyFromBase58(pool.QuoteVault)
	marketProgram := solana.MustPublicKeyFromBase58(pool.MarketProgramID)
	market := solana.MustPublicKeyFromBase58(pool.MarketID)
	marketBids := solana.MustPublicKeyFromBase58(pool.MarketBids)
	marketAsks := solana.MustPublicKeyFromBase58(pool.MarketAsks)
	marketEventQueue := solana.MustPublicKeyFromBase58(pool.MarketEventQueue)
	marketBaseVault := solana.MustPublicKeyFromBase58(pool.MarketBaseVault)
	marketQuoteVault := solana.MustPublicKeyFromBase58(pool.MarketQuoteVault)
	marketAuthority := solana.MustPublicKeyFromBase58(pool.MarketAuthority)
	
	// Build accounts array
	accounts := []*solana.AccountMeta{
		{PublicKey: token.ProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: poolPubkey, IsSigner: false, IsWritable: true},
		{PublicKey: authority, IsSigner: false, IsWritable: false},
		{PublicKey: openOrders, IsSigner: false, IsWritable: true},
		{PublicKey: targetOrders, IsSigner: false, IsWritable: true},
		{PublicKey: baseVault, IsSigner: false, IsWritable: true},
		{PublicKey: quoteVault, IsSigner: false, IsWritable: true},
		{PublicKey: marketProgram, IsSigner: false, IsWritable: false},
		{PublicKey: market, IsSigner: false, IsWritable: true},
		{PublicKey: marketBids, IsSigner: false, IsWritable: true},
		{PublicKey: marketAsks, IsSigner: false, IsWritable: true},
		{PublicKey: marketEventQueue, IsSigner: false, IsWritable: true},
		{PublicKey: marketBaseVault, IsSigner: false, IsWritable: true},
		{PublicKey: marketQuoteVault, IsSigner: false, IsWritable: true},
		{PublicKey: marketAuthority, IsSigner: false, IsWritable: false},
		{PublicKey: userSourceTokenAccount, IsSigner: false, IsWritable: true},
		{PublicKey: userDestTokenAccount, IsSigner: false, IsWritable: true},
		{PublicKey: userOwner, IsSigner: true, IsWritable: false},
	}
	
	return solana.NewInstruction(
		solana.MustPublicKeyFromBase58(pool.ProgramID),
		accounts,
		buf.Bytes(),
	), nil
}

// ExecuteRaydiumSwap executes a swap using Raydium
func ExecuteRaydiumSwap(
	ctx context.Context,
	client *rpc.Client,
	wallet solana.PrivateKey,
	poolID string,
	amountIn float64,
	minAmountOut uint64,
	side string,
) (string, error) {
	// Get pool info from API
	pool, err := GetRaydiumPoolInfo(poolID)
	if err != nil {
		return "", fmt.Errorf("failed to get pool info: %w", err)
	}
	
	fmt.Printf("\nPool Info from API:\n")
	fmt.Printf("Pool ID: %s\n", pool.ID)
	fmt.Printf("Authority: %s\n", pool.Authority)
	fmt.Printf("Base Mint: %s\n", pool.BaseMint)
	fmt.Printf("Quote Mint: %s\n", pool.QuoteMint)
	
	// Determine source and destination mints
	var sourceMint, destMint solana.PublicKey
	var amountInRaw uint64
	
	baseMint := solana.MustPublicKeyFromBase58(pool.BaseMint)
	quoteMint := solana.MustPublicKeyFromBase58(pool.QuoteMint)
	
	if side == "buy" {
		// Buying token with SOL (assuming base is WSOL)
		sourceMint = baseMint
		destMint = quoteMint
		amountInRaw = uint64(amountIn * 1e9) // SOL has 9 decimals
	} else {
		// Selling token for SOL
		sourceMint = quoteMint
		destMint = baseMint
		amountInRaw = uint64(amountIn * float64(pool.QuoteDecimals))
	}
	
	// Get or create ATAs
	sourceAta, _, err := solana.FindAssociatedTokenAddress(wallet.PublicKey(), sourceMint)
	if err != nil {
		return "", err
	}
	destAta, _, err := solana.FindAssociatedTokenAddress(wallet.PublicKey(), destMint)
	if err != nil {
		return "", err
	}
	
	instructions := []solana.Instruction{}
	
	// Check and create ATAs if needed
	// ... (ATA creation logic here)
	
	// Create swap instruction
	swapIx, err := CreateRaydiumSwapInstruction(
		pool,
		sourceAta,
		destAta,
		wallet.PublicKey(),
		amountInRaw,
		minAmountOut,
	)
	if err != nil {
		return "", err
	}
	instructions = append(instructions, swapIx)
	
	// Get latest blockhash
	latestBlockhash, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}
	
	// Build and sign transaction
	tx, err := solana.NewTransaction(
		instructions,
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(wallet.PublicKey()),
	)
	if err != nil {
		return "", err
	}
	
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(wallet.PublicKey()) {
			return &wallet
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	
	// Send transaction
	sig, err := client.SendTransactionWithOpts(
		ctx,
		tx,
		rpc.TransactionOpts{
			SkipPreflight: false,
			PreflightCommitment: rpc.CommitmentFinalized,
		},
	)
	if err != nil {
		return "", err
	}
	
	// Wait for confirmation
	time.Sleep(2 * time.Second)
	
	return sig.String(), nil
}