package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port            string
	DatabaseURL     string
	WorkerCount     int
	ChannelCapacity int
}

func Load() (*Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	workerCount := 8
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid WORKER_COUNT: %s", v)
		}
		workerCount = n
	}

	channelCapacity := 1000
	if v := os.Getenv("CHANNEL_CAPACITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid CHANNEL_CAPACITY: %s", v)
		}
		channelCapacity = n
	}

	return &Config{
		Port:            port,
		DatabaseURL:     dbURL,
		WorkerCount:     workerCount,
		ChannelCapacity: channelCapacity,
	}, nil
}
