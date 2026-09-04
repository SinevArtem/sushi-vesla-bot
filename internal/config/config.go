package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken string
	LogLevel      string
	Port          string
	Environment   string // development, production
}

func Load() *Config {
	// Загружаем .env если есть
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ .env файл не найден, используем системные переменные")
	}

	return &Config{
		TelegramToken: getEnv("TELEGRAM_TOKEN", ""),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		Port:          getEnv("PORT", "8080"),
		Environment:   getEnv("ENVIRONMENT", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
