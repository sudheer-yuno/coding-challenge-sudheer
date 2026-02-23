// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

type PayoutItem struct {
	VendorID    string  `json:"vendor_id"`
	VendorName  string  `json:"vendor_name"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	BankAccount string  `json:"bank_account"`
	BankName    string  `json:"bank_name"`
}

type CreateBatchReq struct {
	Payouts []PayoutItem `json:"payouts"`
}

func main() {
	count := 1000
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			count = n
		}
	}

	baseURL := "http://localhost:8080"
	if u := os.Getenv("BASE_URL"); u != "" {
		baseURL = u
	}

	rand.Seed(time.Now().UnixNano())

	currencies := []string{"IDR", "PHP", "VND"}
	banks := []string{"BCA", "Mandiri", "BNI", "BDO", "Metrobank", "Vietcombank", "Techcombank"}
	regions := []string{"ID", "PH", "VN"}

	payouts := make([]PayoutItem, count)
	for i := 0; i < count; i++ {
		region := regions[rand.Intn(len(regions))]
		payouts[i] = PayoutItem{
			VendorID:    fmt.Sprintf("vendor_%s_%05d", region, i+1),
			VendorName:  fmt.Sprintf("Vendor %s #%d", region, i+1),
			Amount:      float64(rand.Intn(500000)+10000) / 100.0,
			Currency:    currencies[rand.Intn(len(currencies))],
			BankAccount: fmt.Sprintf("%s%012d", region, rand.Int63n(999999999999)),
			BankName:    banks[rand.Intn(len(banks))],
		}
	}

	body, _ := json.Marshal(CreateBatchReq{Payouts: payouts})

	// Create batch
	resp, err := http.Post(baseURL+"/api/v1/batches", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating batch: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Create batch response (%d):\n%s\n", resp.StatusCode, string(respBody))

	// Parse batch ID and start it
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	if batchID, ok := result["batch_id"].(string); ok {
		fmt.Printf("\nStarting batch %s...\n", batchID)
		resp2, err := http.Post(baseURL+"/api/v1/batches/"+batchID+"/start", "application/json", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting batch: %v\n", err)
			os.Exit(1)
		}
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)
		fmt.Printf("Start response (%d):\n%s\n", resp2.StatusCode, string(body2))

		fmt.Printf("\nMonitor at: GET %s/api/v1/batches/%s\n", baseURL, batchID)
		fmt.Printf("Failed payouts: GET %s/api/v1/batches/%s/payouts?status=failed\n", baseURL, batchID)
	}
}
