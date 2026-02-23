//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type PayoutItem struct {
	VendorID       string   `json:"vendor_id"`
	VendorName     string   `json:"vendor_name"`
	Amount         float64  `json:"amount"`
	Currency       string   `json:"currency"`
	BankAccount    string   `json:"bank_account"`
	BankName       string   `json:"bank_name"`
	TransactionIDs []string `json:"transaction_ids"`
}

type CreateBatchReq struct {
	Payouts []PayoutItem `json:"payouts"`
}

func main() {
	baseURL := "http://localhost:8080"
	if u := os.Getenv("BASE_URL"); u != "" {
		baseURL = u
	}

	rand.Seed(time.Now().UnixNano())

	// Create 3 batches as required: small (100), medium (1000), large (5000)
	batches := []struct {
		name  string
		count int
	}{
		{"Small Batch (100 payouts)", 100},
		{"Medium Batch (1,000 payouts)", 1000},
		{"Large Batch (5,000 payouts)", 5000},
	}

	for i, b := range batches {
		fmt.Printf("\n========================================\n")
		fmt.Printf("Creating %s...\n", b.name)
		fmt.Printf("========================================\n")

		batchID := createBatch(baseURL, b.count, i)
		if batchID == "" {
			fmt.Fprintf(os.Stderr, "Failed to create batch %d\n", i+1)
			continue
		}

		fmt.Printf("  Batch ID: %s\n", batchID)
		fmt.Printf("  Payouts:  %d\n", b.count)

		// Start processing (except the large batch — leave it for resumability demo)
		if i < 2 {
			fmt.Printf("  Starting processing...\n")
			startBatch(baseURL, batchID)
		} else {
			fmt.Printf("\n  ⚠️  Large batch NOT started automatically.\n")
			fmt.Printf("  Use it for the resumability demo:\n")
			fmt.Printf("    1. Start:  curl -X POST %s/api/v1/batches/%s/start\n", baseURL, batchID)
			fmt.Printf("    2. Watch:  curl %s/api/v1/batches/%s\n", baseURL, batchID)
			fmt.Printf("    3. Kill the server mid-processing (Ctrl+C)\n")
			fmt.Printf("    4. Restart: make run\n")
			fmt.Printf("    5. Resume: curl -X POST %s/api/v1/batches/%s/start\n", baseURL, batchID)
		}
	}

	fmt.Printf("\n========================================\n")
	fmt.Println("All batches created! Use the batch IDs above to query status.")
	fmt.Printf("========================================\n")
}

func createBatch(baseURL string, count, batchNum int) string {
	currencies := []string{"IDR", "PHP", "VND"}
	banks := map[string][]string{
		"IDR": {"BCA", "Mandiri", "BNI", "BRI", "CIMB Niaga"},
		"PHP": {"BDO", "Metrobank", "BPI", "UnionBank", "Landbank"},
		"VND": {"Vietcombank", "Techcombank", "VPBank", "MB Bank", "ACB"},
	}
	regions := []string{"ID", "PH", "VN"}
	categories := []string{"crafts", "electronics", "clothing", "food", "accessories"}

	payouts := make([]PayoutItem, count)
	for i := 0; i < count; i++ {
		region := regions[rand.Intn(len(regions))]
		currency := currencies[rand.Intn(len(currencies))]
		bankList := banks[currency]
		category := categories[rand.Intn(len(categories))]

		// Generate 1-5 transaction IDs per payout (accumulated sales)
		numTxns := 1 + rand.Intn(5)
		txnIDs := make([]string, numTxns)
		for j := 0; j < numTxns; j++ {
			txnIDs[j] = fmt.Sprintf("TXN-%s-%d-%05d-%03d", region, batchNum, i, j)
		}

		// Realistic amounts per currency
		var amount float64
		switch currency {
		case "IDR":
			amount = float64(50000+rand.Intn(9950000)) // 50K - 10M IDR
		case "PHP":
			amount = float64(500+rand.Intn(49500)) + float64(rand.Intn(100))/100.0
		case "VND":
			amount = float64(100000 + rand.Intn(49900000)) // 100K - 50M VND
		}

		payouts[i] = PayoutItem{
			VendorID:       fmt.Sprintf("KV-%s-%s-%05d", region, category[:3], i+1),
			VendorName:     fmt.Sprintf("%s %s Vendor #%d", region, capitalize(category), i+1),
			Amount:         amount,
			Currency:       currency,
			BankAccount:    fmt.Sprintf("%s****%04d", region, rand.Intn(10000)),
			BankName:       bankList[rand.Intn(len(bankList))],
			TransactionIDs: txnIDs,
		}
	}

	body, _ := json.Marshal(CreateBatchReq{Payouts: payouts})

	resp, err := http.Post(baseURL+"/api/v1/batches", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	if batchID, ok := result["batch_id"].(string); ok {
		return batchID
	}
	fmt.Fprintf(os.Stderr, "Unexpected response: %s\n", string(respBody))
	return ""
}

func startBatch(baseURL, batchID string) {
	resp, err := http.Post(baseURL+"/api/v1/batches/"+batchID+"/start", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting batch: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("  → %s\n", string(body))
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}
