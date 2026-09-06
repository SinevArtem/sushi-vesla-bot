package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken string
	DatabaseURL   string
	DataPath      string
	LogLevel      string
	Port          string
	Environment   string
	SearchRadius  int
	MaxSegments   int
	OSRMURL       string // <-- НОВОЕ
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ .env файл не найден, используем системные переменные")
	}

	return &Config{
		TelegramToken: getEnv("TELEGRAM_TOKEN", ""),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/sushi_vesla"),
		DataPath:      getEnv("DATA_PATH", "./data"),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		Port:          getEnv("PORT", "8080"),
		Environment:   getEnv("ENVIRONMENT", "development"),
		SearchRadius:  getEnvAsInt("SEARCH_RADIUS", 20000),
		MaxSegments:   getEnvAsInt("MAX_SEGMENTS", 3),
		OSRMURL:       getEnv("OSRM_URL", "https://router.project-osrm.org"), // <-- НОВОЕ
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
