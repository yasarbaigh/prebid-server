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
	case "encrypt", "e":
		res, err := cryptoutil.Encrypt(text)
		run("Encryption", res, err)
	case "decrypt", "d":
		res, err := cryptoutil.Decrypt(text)
		run("Decryption", res, err)
	case "encrypt-c", "ec":
		res, err := cryptoutil.EncryptCompressed(text)
		run("Compressed Encryption", res, err)
	case "decrypt-c", "dc":
		res, err := cryptoutil.DecryptCompressed(text)
		run("Decrypted Decompression", res, err)
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
	fmt.Println("\033[1mCrypto Tool - Simple Encrypt/Decrypt Utility\033[0m")
	fmt.Println("\nUsage:")
	fmt.Println("  go run crypto_tool.go <command> <text>")
	fmt.Println("  go run crypto_tool.go <command> -text=\"your text\"")
	fmt.Println("\nCommands:")
	fmt.Println("  encrypt (e)    - Standard AES-GCM encryption (for 'p' and 'p' params)")
	fmt.Println("  decrypt (d)    - Standard AES-GCM decryption")
	fmt.Println("  encrypt-c (ec) - Compressed + AES-GCM encryption (for 'd' param)")
	fmt.Println("  decrypt-c (dc) - Decrypt + Decompression")
	fmt.Println("\nExamples:")
	fmt.Println("  go run z_cd_hints/crypto_tool/crypto_tool.go encrypt \"tid=1&sid=2\"")
	fmt.Println("  go run z_cd_hints/crypto_tool/crypto_tool.go decrypt \"AQIDBAU...\"")
	fmt.Println("  go run z_cd_hints/crypto_tool/crypto_tool.go ec \"https://dsp-nurl.com\"")
}
