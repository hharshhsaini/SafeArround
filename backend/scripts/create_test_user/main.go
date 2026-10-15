package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/obsidiannnn/Safe-Around/backend/internal/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	ID              uint       `gorm:"primaryKey"`
	Phone           string     `gorm:"uniqueIndex"`
	Password        string
	Name            string
	Email           string
	IsPhoneVerified bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time `gorm:"index"`
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

	// Hash password
	hashedPassword, err := utils.HashPassword("Test@123")
	if err != nil {
		log.Fatal("Failed to hash password:", err)
	}

	// Create test user
	testUser := User{
		Phone:           "+919999999999",
		Password:        hashedPassword,
		Name:            "Test User",
		Email:           "test@safearound.com",
		IsPhoneVerified: true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Check if user already exists
	var existing User
	result := db.Where("phone = ?", testUser.Phone).First(&existing)
	if result.Error == nil {
		// User exists, update password
		existing.Password = hashedPassword
		existing.Name = testUser.Name
		existing.Email = testUser.Email
		if err := db.Save(&existing).Error; err != nil {
			log.Fatal("Failed to update user:", err)
		}
		fmt.Println("\n✅ Test user updated successfully!")
	} else {
		// Create new user
		if err := db.Create(&testUser).Error; err != nil {
			log.Fatal("Failed to create user:", err)
		}
		fmt.Println("\n✅ Test user created successfully!")
	}

	fmt.Println("\n=== Test User Credentials ===")
	fmt.Println("Phone: +919999999999")
	fmt.Println("Password: Test@123")
	fmt.Print("============================\n\n")
}
