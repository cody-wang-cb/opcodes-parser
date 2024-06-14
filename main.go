package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/joho/godotenv"
)

type StructLog struct {
	PC      uint64            `json:"pc"`
	Op      string            `json:"op"`
	Gas     uint64            `json:"gas"`
	GasCost uint64            `json:"gasCost"`
	Depth   int               `json:"depth"`
	Error   string            `json:"error,omitempty"`
	Stack   []string          `json:"stack"`
	Memory  []string          `json:"memory"`
	Storage map[string]string `json:"storage"`
}

type TraceResult struct {
	Gas         uint64      `json:"gas"`
	ReturnValue string      `json:"returnValue"`
	StructLogs  []StructLog `json:"structLogs"`
}

func printWithTimestamp(message string) {
	// Get the current time
	currentTime := time.Now()

	// Format the time as needed. Example format: "2006-01-02 15:04:05"
	timestamp := currentTime.Format("2006-01-02 15:04:05")

	// Print the message with the timestamp
	fmt.Printf("[%s] %s\n", timestamp, message)
}

func main() {
	var startBlockNum int
	var endBlockNum int
	var chain string

	flag.IntVar(&startBlockNum, "start-block-num", 11443817, "the block to start from")
	flag.IntVar(&endBlockNum, "end-block-num", 11443817, "the block to end at")
	flag.StringVar(&chain, "chain", "base", "chain to use")

	flag.Parse()

	fmt.Println("Starting block: ", startBlockNum)
	fmt.Println("End block: ", endBlockNum)
	fmt.Println("Chain: ", chain)

	// Load the .env file
    err := godotenv.Load()
    if err != nil {
        log.Fatalf("Error loading .env file")
    }

	var clientLocation string
	if chain == "base" {
		clientLocation = os.Getenv("BASE_RPC_URL")
	} else {
		clientLocation = os.Getenv("OPTIMISM_RPC_URL")
	}

	client, err := rpc.Dial(clientLocation)
	if err != nil {
		// Cannot connect to local node for some reason
		log.Fatal(err)
	}

	var blockNum int
	for blockNum = startBlockNum; blockNum <= endBlockNum; blockNum++ {
		printWithTimestamp(strconv.Itoa(blockNum))
		var result []map[string]interface{}
		err = client.CallContext(context.Background(), &result, "debug_traceBlockByNumber", fmt.Sprintf("0x%x", blockNum), map[string]interface{}{})
		if err != nil {
			log.Fatalf("Failed to trace block: %v", err)
		}

		opcodes := make(map[string]int)
		opcodesGasCost := make(map[string]float64)
		averageOpcodesGasCost := make(map[string]float64)
		maxOpcodesGasCost := make(map[string]float64)
		minOpcodesGasCost := make(map[string]float64)

		for _, txTrace := range result {
			res := txTrace["result"].(map[string]interface{})
			structLogs := res["structLogs"].([]interface{})
			for _, logEntry := range structLogs {
				log := logEntry.(map[string]interface{})
				opcodes[log["op"].(string)]++
				gasCost := float64(log["gasCost"].(float64))
				opcodesGasCost[log["op"].(string)] += gasCost
				if maxOpcodesGasCost[log["op"].(string)] < gasCost {
					maxOpcodesGasCost[log["op"].(string)] = gasCost
				}
				if minOpcodesGasCost[log["op"].(string)] == 0 || minOpcodesGasCost[log["op"].(string)] > gasCost {
					minOpcodesGasCost[log["op"].(string)] = gasCost
				}
			}
		}

		// Calculate average gas cost for each opcode
		for opcode, gas := range opcodesGasCost {
			averageOpcodesGasCost[opcode] = gas / float64(opcodes[opcode])
		}

		dirName := fmt.Sprintf("./results/%s/%s_%s", chain, strconv.Itoa(startBlockNum), strconv.Itoa(endBlockNum))
		err := os.MkdirAll(dirName, os.ModePerm)
		if err != nil {
			log.Fatalf("Error creating directory: %v", err)
		}

		writeJSON(opcodes, filepath.Join(dirName, "opcodesDistribution.json"))
		writeJSON(averageOpcodesGasCost, filepath.Join(dirName, "averageOpcodesGasCost.json"))
		writeJSON(maxOpcodesGasCost, filepath.Join(dirName, "maxOpcodesGasCost.json"))
		writeJSON(minOpcodesGasCost, filepath.Join(dirName, "minOpcodesGasCost.json"))
		writeJSON(opcodesGasCost, filepath.Join(dirName, "totalOpcodesGasCost.json"))
	}

	defer client.Close()
}


func writeJSON(data interface{}, fileName string) {
	// Convert map to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling to JSON: %v", err)
	}

	// Write JSON to a file
	file, err := os.Create(fileName)
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer file.Close()

	_, err = file.Write(jsonData)
	if err != nil {
		log.Fatalf("Error writing to file: %v", err)
	}
}
