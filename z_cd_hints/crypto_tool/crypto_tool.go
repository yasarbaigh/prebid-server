package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

func main() {
	encryptCmd := flag.NewFlagSet("encrypt", flag.ExitOnError)
	decryptCmd := flag.NewFlagSet("decrypt", flag.ExitOnError)
	encryptCCmd := flag.NewFlagSet("encrypt-c", flag.ExitOnError)
	decryptCCmd := flag.NewFlagSet("decrypt-c", flag.ExitOnError)

	eText := encryptCmd.String("text", "", "Text to encrypt")
	dText := decryptCmd.String("text", "", "Ciphertext to decrypt")
	ecText := encryptCCmd.String("text", "", "Text to compress and encrypt")
	dcText := decryptCCmd.String("text", "", "Ciphertext to decrypt and decompress")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "encrypt":
		encryptCmd.Parse(os.Args[2:])
		run(cryptoutil.Encrypt(*eText))
	case "decrypt":
		decryptCmd.Parse(os.Args[2:])
		run(cryptoutil.Decrypt(*dText))
	case "encrypt-c":
		encryptCCmd.Parse(os.Args[2:])
		run(cryptoutil.EncryptCompressed(*ecText))
	case "decrypt-c":
		decryptCCmd.Parse(os.Args[2:])
		run(cryptoutil.DecryptCompressed(*dcText))
	default:
		printUsage()
		os.Exit(1)
	}
}

func run(result string, err error) {
	if err != nil {
		fmt.Printf("\033[31mError:\033[0m %v\n", err)
		return
	}
	fmt.Printf("\033[32mResult:\033[0m\n%s\n", result)
}

func printUsage() {
	fmt.Println("Usage: crypto_tool <command> -text <value>")
	fmt.Println("\nCommands:")
	fmt.Println("  encrypt    - Standard AES-GCM encryption (used for 'p' and 'pd')")
	fmt.Println("  decrypt    - Standard AES-GCM decryption")
	fmt.Println("  encrypt-c  - Compressed + AES-GCM encryption (used for 'd')")
	fmt.Println("  decrypt-c  - Decrypt + Decompression")
}
