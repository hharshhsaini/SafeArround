package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	ID       uint   `gorm:"primaryKey"`
	Phone    string `gorm:"uniqueIndex"`
	Password string
	Name     string
	Email    string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run ./scripts/test_login <phone> <password>  (from backend/)")
		os.Exit(1)
	}

	phone := os.Args[1]
	password := os.Args[2]

	// Load .env file
	if err := godotenv.Load(".env"); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Build connection string
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_SSLMODE"),
	)

	// Connect to database
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Find user
	var user User
	if err := db.Where("phone = ?", phone).First(&user).Error; err != nil {
		log.Fatal("User not found:", err)
	}

	fmt.Printf("\nUser found: %s (%s)\n", user.Name, user.Phone)
	fmt.Printf("Has password: %v\n", user.Password != "")
	fmt.Printf("Password hash length: %d\n", len(user.Password))
	
	// Test password
	match := utils.ComparePassword(user.Password, password)
	fmt.Printf("Password match: %v\n\n", match)

	if match {
		fmt.Println("✅ Login would succeed")
	} else {
		fmt.Println("❌ Login would fail - password doesn't match")
	}
}
