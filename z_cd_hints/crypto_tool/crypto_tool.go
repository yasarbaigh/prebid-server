package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

// example to run

// go run z_cd_hints/crypto_tool/crypto_tool.go encrypt -text "tid=1&siid=101&dp=5.50"
// go run z_cd_hints/crypto_tool/crypto_tool.go decrypt -text "PASTE_THE_ENCRYPTED_STRING_HERE"

// AESKey is used from cryptoutil.AESKey

func main() {
	encryptCmd := flag.NewFlagSet("encrypt", flag.ExitOnError)
	decryptCmd := flag.NewFlagSet("decrypt", flag.ExitOnError)

	encryptText := encryptCmd.String("text", "", "Text to encrypt")
	decryptText := decryptCmd.String("text", "", "Ciphertext to decrypt")

	if len(os.Args) < 2 {
		fmt.Println("expected 'encrypt' or 'decrypt' subcommands")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "encrypt":
		encryptCmd.Parse(os.Args[2:])
		if *encryptText == "" {
			fmt.Println("Usage: crypto_tool encrypt -text <plaintext>")
			return
		}
		result, err := cryptoutil.Encrypt(*encryptText)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println(result)

	case "decrypt":
		decryptCmd.Parse(os.Args[2:])
		if *decryptText == "" {
			fmt.Println("Usage: crypto_tool decrypt -text <ciphertext>")
			return
		}
		result, err := cryptoutil.Decrypt(*decryptText)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println(result)

	default:
		fmt.Println("expected 'encrypt' or 'decrypt' subcommands")
		os.Exit(1)
	}
}

func encrypt(plaintext string) (string, error) {
	return cryptoutil.Encrypt(plaintext)
}

func decrypt(cryptoText string) (string, error) {
	return cryptoutil.Decrypt(cryptoText)
}
