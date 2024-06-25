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
	var checkpoint int

	flag.IntVar(&startBlockNum, "start-block-num", 11443817, "the block to start from")
	flag.IntVar(&endBlockNum, "end-block-num", 11443817, "the block to end at")
	flag.IntVar(&checkpoint, "checkpoint", -1, "checkpoint to load")
	flag.StringVar(&chain, "chain", "base", "chain to use")

	flag.Parse()

	fmt.Println("Starting block: ", startBlockNum)
	fmt.Println("End block: ", endBlockNum)
	fmt.Println("Chain: ", chain)
	fmt.Println("Checkpoint: ", checkpoint)

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
	opcodes := make(map[string]int)
	opcodesGasCost := make(map[string]float64)
	averageOpcodesGasCost := make(map[string]float64)
	maxOpcodesGasCost := make(map[string]float64)
	minOpcodesGasCost := make(map[string]float64)

	if checkpoint > 0 {
		var err error
		dirName := fmt.Sprintf("./results/%s/%s_%s/%s_%s", chain, strconv.Itoa(startBlockNum), strconv.Itoa(endBlockNum), strconv.Itoa(startBlockNum), strconv.Itoa(checkpoint))
		opcodes, err = LoadJSONIntoIntMap(filepath.Join(dirName, "opcodesDistribution.json"))
		if err != nil {
			log.Fatalf("Error loading JSON file: %v", err)
		}

		averageOpcodesGasCost, err = LoadJSONIntoFloat64Map(filepath.Join(dirName, "averageOpcodesGasCost.json"))
		if err != nil {
			log.Fatalf("Error loading JSON file: %v", err)
		}

		maxOpcodesGasCost, err = LoadJSONIntoFloat64Map(filepath.Join(dirName, "maxOpcodesGasCost.json"))
		if err != nil {
			log.Fatalf("Error loading JSON file: %v", err)
		}

		minOpcodesGasCost, err = LoadJSONIntoFloat64Map(filepath.Join(dirName, "minOpcodesGasCost.json"))
		if err != nil {
			log.Fatalf("Error loading JSON file: %v", err)
		}

		opcodesGasCost, err = LoadJSONIntoFloat64Map(filepath.Join(dirName, "totalOpcodesGasCost.json"))
		if err != nil {
			log.Fatalf("Error loading JSON file: %v", err)
		}

		// Output the loaded data for verification
		fmt.Printf("opcodes: %v\n", opcodes)
		fmt.Printf("averageOpcodesGasCost: %v\n", averageOpcodesGasCost)
		fmt.Printf("maxOpcodesGasCost: %v\n", maxOpcodesGasCost)
		fmt.Printf("minOpcodesGasCost: %v\n", minOpcodesGasCost)
		fmt.Printf("opcodesGasCost: %v\n", opcodesGasCost)

		blockNum = checkpoint
	} else {
		blockNum = startBlockNum
	}

	for ; blockNum <= endBlockNum; blockNum++ {
		printWithTimestamp(strconv.Itoa(blockNum))

		// checkpoint every 100 blocks
		if ((blockNum - startBlockNum) != 0 && (blockNum - startBlockNum) % 100 == 0) {
			// Calculate the current average gas cost for each opcode
			for opcode, gas := range opcodesGasCost {
				averageOpcodesGasCost[opcode] = gas / float64(opcodes[opcode])
			}
			dirName := fmt.Sprintf("./results/%s/%s_%s/%s_%s", chain, strconv.Itoa(startBlockNum), strconv.Itoa(endBlockNum), strconv.Itoa(startBlockNum), strconv.Itoa(blockNum))
			saveResults(dirName, opcodes, averageOpcodesGasCost, maxOpcodesGasCost, minOpcodesGasCost, opcodesGasCost)
		}

		var result []map[string]interface{}

		numTries := 2
		blockNumHex := fmt.Sprintf("0x%x", blockNum)
		for {
			if numTries == 0 {
				panic("Failed to trace block after 2 tries")
			}
			err = client.CallContext(context.Background(), &result, "debug_traceBlockByNumber", blockNumHex)
			if err != nil {
				log.Println("Failed to trace block: %v", err)
				time.Sleep(1 * time.Second)
				numTries -= 1
				continue
			}
			break
		}
		
		if numTries == 0 {
			log.Println("Failed to trace block %d after 2 tries, using tx trace instead", blockNum)
			var result json.RawMessage
			err := client.CallContext(context.Background(), &result, "eth_getBlockByNumber", blockNumHex, false)
			if err != nil {
				log.Fatalf("Failed to get block: %v", err)
			}

			var block map[string]interface{}
			err = json.Unmarshal(result, &block)
			if err != nil {
				log.Fatalf("Failed to unmarshal block: %v", err)
			}
			
			txs := block["transactions"]
			for _, txHash := range txs.([]interface{}) {
				var txResult map[string]interface{}
				err := client.CallContext(context.Background(), &txResult, "debug_traceTransaction", txHash)
				if err != nil {
					log.Fatalf("Failed to trace transaction for hash %s: %v", txHash, err)
				}
				for _, logEntry := range txResult["structLogs"].([]interface{}) {
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
		} else {
			callAllocatedGas := 0
			getCallUsedGas := false
			callOperation := ""
			prevLog := map[string]interface{}{}
			for _, txTrace := range result[:30] {
				res := txTrace["result"].(map[string]interface{})
				structLogs := res["structLogs"].([]interface{})
				for _, logEntry := range structLogs {
					log := logEntry.(map[string]interface{})
					opcodes[log["op"].(string)]++
					gasCost := float64(log["gasCost"].(float64))

					// Within a CALL, DELEGATECALL, STATICCALL
					// - The gascost of the CALL = the amount allocated - the amount remaining before the first operation in the call
					// - The total cost of the CALL = the amount of gas left after it returns - the amount of available gas before the call
					// - The total cost of the CALL execution = amount of gas left after it returns - the gas cost of the CALL - amount of gas left before the call
					// - use the gas diff iff the next depth is somehow the same
					if getCallUsedGas {
						callGasCost := callAllocatedGas - int(log["gas"].(float64))
						if log["depth"].(float64) == prevLog["depth"].(float64) {
							callGasCost = int(prevLog["gas"].(float64)) - int(log["gas"].(float64))
						}
						opcodesGasCost[callOperation] += float64(callGasCost)
						if maxOpcodesGasCost[callOperation] < float64(callGasCost) {
							maxOpcodesGasCost[callOperation] = float64(callGasCost)
						}
						if minOpcodesGasCost[callOperation] == 0 || minOpcodesGasCost[callOperation] > float64(callGasCost) {
							minOpcodesGasCost[callOperation] = float64(callGasCost)
						}
						getCallUsedGas = false
						if callGasCost < 0 {
							fmt.Println(prevLog)
							fmt.Println(log)
							fmt.Println(structLogs)
							panic("Negative gas cost")
						}
					}
					
					// If there's a new call, track the total gas for the call and update it in the next log
					if (log["op"].(string) == "CALL") || (log["op"].(string) == "DELEGATECALL") || (log["op"].(string) == "STATICCALL") {
						getCallUsedGas = true
						callAllocatedGas = int(log["gasCost"].(float64))
						callOperation = log["op"].(string)
						prevLog = log
						continue
					}

					opcodesGasCost[log["op"].(string)] += gasCost
					if maxOpcodesGasCost[log["op"].(string)] < gasCost {
						maxOpcodesGasCost[log["op"].(string)] = gasCost
					}
					if minOpcodesGasCost[log["op"].(string)] == 0 || minOpcodesGasCost[log["op"].(string)] > gasCost {
						minOpcodesGasCost[log["op"].(string)] = gasCost
					}
					prevLog = log
				}
			}
		}
	}

	// Calculate average gas cost for each opcode
	for opcode, gas := range opcodesGasCost {
		averageOpcodesGasCost[opcode] = gas / float64(opcodes[opcode])
	}

	dirName := fmt.Sprintf("./results/%s/%s_%s", chain, strconv.Itoa(startBlockNum), strconv.Itoa(endBlockNum))
	saveResults(dirName, opcodes, averageOpcodesGasCost, maxOpcodesGasCost, minOpcodesGasCost, opcodesGasCost)

	defer client.Close()
}

func saveResults(dirName string, opcodes map[string]int, averageOpcodesGasCost map[string]float64, maxOpcodesGasCost map[string]float64, minOpcodesGasCost map[string]float64, opcodesGasCost map[string]float64) {
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

// LoadJSONIntoIntMap loads a JSON file and decodes it into a map[string]int
func LoadJSONIntoIntMap(filename string) (map[string]int, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %w", err)
	}
	defer file.Close()

	var data map[string]int
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode JSON data: %w", err)
	}

	return data, nil
}

// LoadJSONIntoFloat64Map loads a JSON file and decodes it into a map[string]float64
func LoadJSONIntoFloat64Map(filename string) (map[string]float64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %w", err)
	}
	defer file.Close()

	var data map[string]float64
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode JSON data: %w", err)
	}

	return data, nil
}
