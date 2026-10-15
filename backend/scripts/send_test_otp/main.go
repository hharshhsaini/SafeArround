package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/logger"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/twilio"
)

// Sends a real OTP through Twilio Verify to check the Twilio setup.
// Usage (from backend/): go run ./scripts/send_test_otp <phone, e.g. +919876543210>
func main() {
	// Load environment variables from .env file
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("Note: Error loading .env file, relying on system environment variables")
	}

	logger.InitLogger("info", true)

	sid := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")

	if sid == "" || sid == "your_twilio_sid" {
		fmt.Println("Error: Please set your actual Twilio SID and Token in the .env file")
		os.Exit(1)
	}

	if len(os.Args) < 2 || os.Args[1] == "" {
		fmt.Println("Usage: go run ./scripts/send_test_otp <phone, e.g. +919876543210>  (from backend/)")
		os.Exit(1)
	}
	toPhone := os.Args[1]

	fmt.Println("Initializing Twilio Client...")
	client := twilio.NewClient(sid, token)

	fmt.Printf("Sending SMS to %s...\n", toPhone)
	
	verifySID := "VAec453b0a41cbb20d1577c8c7ffe8ce64"

	msgID, err := client.SendOTP(toPhone, verifySID)
	
	if err != nil {
		fmt.Printf("Error: Failed to deliver message. Reason: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Success! OTP Message delivered. Verify Token Hook ID: %s\n", msgID)
}
