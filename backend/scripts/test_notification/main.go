package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/obsidiannnn/Safe-Around/backend/pkg/fcm"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(".env"); err != nil {
		log.Println("No .env file found, using system environment")
	}

	// Test token: an Expo push token, or an FCM token if FCM_SERVICE_ACCOUNT_JSON is set
	if len(os.Args) < 2 || os.Args[1] == "" {
		log.Fatal("Usage: go run ./scripts/test_notification <push_token>  (from backend/)")
	}
	testToken := os.Args[1]

	fmt.Println("🔔 Testing push notification...")
	fmt.Printf("Token: %s\n", testToken)

	client, err := fcm.NewClient(os.Getenv("FCM_SERVICE_ACCOUNT_JSON"))
	if err != nil {
		log.Fatalf("❌ Invalid FCM_SERVICE_ACCOUNT_JSON: %v", err)
	}

	// Send test notification
	msgID, err := client.SendNotification(
		testToken,
		"🚨 Test Emergency Alert",
		"This is a test notification from SafeAround backend",
		map[string]string{
			"category":   "EMERGENCY_ALERT",
			"alert_type": "test",
			"test":       "true",
		},
	)

	if err != nil {
		log.Fatalf("❌ Notification failed: %v", err)
	}

	fmt.Printf("✅ Notification sent successfully!\n")
	fmt.Printf("Message ID: %s\n", msgID)
}
