package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
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

	// Query all users
	var users []User
	if err := db.Find(&users).Error; err != nil {
		log.Fatal("Failed to query users:", err)
	}

	fmt.Println("\n=== Users in Database ===")
	for _, u := range users {
		hasPassword := "NO"
		if u.Password != "" {
			hasPassword = "YES"
		}
		fmt.Printf("ID: %d | Phone: %s | Name: %s | Email: %s | Has Password: %s\n",
			u.ID, u.Phone, u.Name, u.Email, hasPassword)
	}
	fmt.Print("========================\n\n")
}
