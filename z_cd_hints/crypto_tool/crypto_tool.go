package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	var text string

	// Handle both flag based and positional arguments
	if len(os.Args) >= 3 {
		if strings.HasPrefix(os.Args[2], "-text") {
			// Flag parsing for -text
			flagSet := flag.NewFlagSet(command, flag.ExitOnError)
			t := flagSet.String("text", "", "Text to process")
			flagSet.Parse(os.Args[2:])
			text = *t
		} else {
			// Positional argument
			text = strings.Join(os.Args[2:], " ")
		}
	}

	if text == "" && command != "help" {
		fmt.Printf("\033[31mError:\033[0m No input text provided\n")
		printUsage()
		os.Exit(1)
	}

	switch command {
	case "url-e", "ue":
		// Production V7: Brotli (Quality 4) for DSP URL ('d' parameter)
		res, err := cryptoutil.EncryptCompressed(text)
		run("URL Encryption (Brotli V7 - 'd')", res, err)
	case "url-d", "ud":
		res, err := cryptoutil.DecryptCompressed(text)
		run("URL Decryption (Brotli V7 - 'd')", res, err)
	case "pay-e", "pe":
		// Production V7: Zlib (BestCompression) for Binary Payload ('x' parameter)
		res, err := cryptoutil.EncryptBinary([]byte(text))
		run("Payload Encryption (Zlib V7 - 'x')", res, err)
	case "pay-d", "pd":
		res, err := cryptoutil.DecryptBinary(text)
		if err == nil {
			run("Payload Decryption (Zlib V7 - 'x')", string(res), nil)
		} else {
			run("Payload Decryption (Zlib V7 - 'x')", "", err)
		}
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("\033[31mError:\033[0m Unknown command '%s'\n", command)
		printUsage()
		os.Exit(1)
	}
}

func run(label string, result string, err error) {
	fmt.Printf("\033[36m=== %s ===\033[0m\n", label)
	if err != nil {
		fmt.Printf("\033[31mStatus:\033[0m Error\n")
		fmt.Printf("\033[31mDetails:\033[0m %v\n", err)
		return
	}
	fmt.Printf("\033[32mStatus:\033[0m Success\n")
	fmt.Printf("\033[32mOutput:\033[0m\n%s\n", result)
}

func printUsage() {
	fmt.Println("\033[1mCrypto Tool V7 - Production Encoding Utility\033[0m")
	fmt.Println("\nUsage:")
	fmt.Println("  go run crypto_tool.go <command> <text>")
	fmt.Println("  go run crypto_tool.go <command> -text=\"your text\"")
	fmt.Println("\nCommands (V7 Strategy):")
	fmt.Println("  url-e (ue) - BROTLI encoding for DSP URLs (parameter 'd')")
	fmt.Println("  url-d (ud) - BROTLI decoding for DSP URLs")
	fmt.Println("  pay-e (pe) - ZLIB encoding for payloads (parameter 'x')")
	fmt.Println("  pay-d (pd) - ZLIB decoding for payloads")
	fmt.Println("\nExamples:")
	fmt.Println("  go run z_cd_hints/crypto_tool/crypto_tool.go ue \"https://dsp.com/win?p=${AUCTION_PRICE}\"")
	fmt.Println("  go run z_cd_hints/crypto_tool/crypto_tool.go pe \"tid=1&sid=2&did=3\"")
	fmt.Println("\n\033[90mNote: AES-GCM and XOR markers have been removed in V7 to maximize transmission speed.\033[0m")
}
