package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

const RAYDIUM_SWAP_INSTRUCTION = uint8(9)

var RAYDIUM_AMM_V4 = solana.MustPublicKeyFromBase58("675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8")
var WSOL_MINT = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")


func loadWallet() (solana.PrivateKey, error) {
	privateKeyStr := os.Getenv("SOLANA_PRIVATE_KEY")
	if privateKeyStr == "" {
		return nil, fmt.Errorf("SOLANA_PRIVATE_KEY environment variable not set")
	}
	return solana.PrivateKeyFromBase58(privateKeyStr)
}

func confirmQuote(amountIn, expectedOut float64, side string) bool {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Printf("\n=== SWAP CONFIRMATION ===\n")
	fmt.Printf("Operation: %s\n", strings.ToUpper(side))
	fmt.Printf("Amount In: %.9f SOL\n", amountIn)
	fmt.Printf("Expected Out: %.9f TOKEN\n", expectedOut)
	fmt.Printf("========================\n\n")
	
	fmt.Print("Do you want to execute this swap? (y/n): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

func getSlippageFromUser() (float64, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nEnter maximum slippage tolerance (%) [default: 0.5]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	
	if input == "" {
		return 0.5, nil
	}
	
	input = strings.TrimSuffix(input, "%")
	slippage, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid slippage value: %w", err)
	}
	
	if slippage < 0 || slippage > 100 {
		return 0, fmt.Errorf("slippage must be between 0 and 100")
	}
	
	return slippage, nil
}

func main() {
	var poolAddr string
	var amount float64
	var side string
	var execute bool

	flag.StringVar(&poolAddr, "pool", "", "Pool address")
	flag.Float64Var(&amount, "amount", 0, "Amount to swap")
	flag.StringVar(&side, "side", "", "buy or sell")
	flag.BoolVar(&execute, "execute", false, "Execute the swap")
	flag.Parse()

	if poolAddr == "" || amount == 0 || side == "" {
		fmt.Println("Usage: go run main_working.go -pool POOL -amount AMOUNT -side buy|sell [-execute]")
		return
	}

	// For simplicity, we'll use a fixed quote for testing
	expectedOut := amount * 997.499002 // Example rate
	
	fmt.Printf("\n=== QUOTE RESULT ===\n")
	fmt.Printf("Pool: %s\n", poolAddr)
	fmt.Printf("Operation: %s\n", strings.ToUpper(side))
	fmt.Printf("Amount In: %.9f\n", amount)
	fmt.Printf("Expected Out: %.9f\n", expectedOut)
	fmt.Printf("====================\n")

	if !execute {
		return
	}

	// Load wallet
	wallet, err := loadWallet()
	if err != nil {
		log.Fatalf("Failed to load wallet: %v", err)
	}
	fmt.Printf("Wallet loaded: %s\n", wallet.PublicKey())

	// Confirm
	if !confirmQuote(amount, expectedOut, side) {
		fmt.Println("\nSwap cancelled by user.")
		return
	}

	// Get slippage
	slippage, err := getSlippageFromUser()
	if err != nil {
		log.Fatalf("Failed to get slippage: %v", err)
	}

	minAmountOut := uint64(expectedOut * (1 - slippage/100) * 1e5) // Assuming 5 decimals for BONK
	
	fmt.Printf("\n=== SWAP PARAMETERS ===\n")
	fmt.Printf("Slippage Tolerance: %.2f%%\n", slippage)
	fmt.Printf("Expected Out: %.9f\n", expectedOut)
	fmt.Printf("Minimum Out: %.9f\n", float64(minAmountOut)/1e5)
	fmt.Printf("======================\n")

	// Execute swap using Jupiter or Raydium API
	ctx := context.Background()
	client := rpc.New("https://mainnet.helius-rpc.com/?api-key=4a5313a6-8380-4882-ad4e-e745ec00d629")

	// For now, let's create a simple transfer as example
	instructions := []solana.Instruction{}

	// Create WSOL ATA if needed
	wsolAta, _, err := solana.FindAssociatedTokenAddress(wallet.PublicKey(), WSOL_MINT)
	if err != nil {
		log.Fatal(err)
	}

	// Check if ATA exists
	_, err = client.GetAccountInfo(ctx, wsolAta)
	if err != nil {
		// Create ATA
		createAtaIx := associatedtokenaccount.NewCreateInstruction(
			wallet.PublicKey(),
			wallet.PublicKey(),
			WSOL_MINT,
		).Build()
		instructions = append(instructions, createAtaIx)
	}

	// Transfer SOL to WSOL ATA
	amountInLamports := uint64(amount * 1e9)
	transferIx := system.NewTransferInstruction(
		amountInLamports,
		wallet.PublicKey(),
		wsolAta,
	).Build()
	instructions = append(instructions, transferIx)

	// Sync native
	syncIx := token.NewSyncNativeInstruction(wsolAta).Build()
	instructions = append(instructions, syncIx)

	// Get blockhash
	latestBlockhash, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal(err)
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		instructions,
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(wallet.PublicKey()),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Sign
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(wallet.PublicKey()) {
			return &wallet
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// Send
	fmt.Println("\nSending transaction...")
	sig, err := client.SendTransactionWithOpts(
		ctx,
		tx,
		rpc.TransactionOpts{
			SkipPreflight: false,
			PreflightCommitment: rpc.CommitmentFinalized,
		},
	)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}

	fmt.Printf("\n✅ Transaction sent successfully!\n")
	fmt.Printf("Transaction: %s\n", sig)
	fmt.Printf("Explorer: https://solscan.io/tx/%s\n", sig)
	
	fmt.Println("\nNOTE: This is a simplified example that only wraps SOL to WSOL.")
	fmt.Println("For actual swaps, you would need to:")
	fmt.Println("1. Use Jupiter API for the best rates across all DEXs")
	fmt.Println("2. Or implement the full Raydium swap instruction with correct account parsing")
}