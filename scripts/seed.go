//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var creators = []string{
	"ninja", "shroud", "pokimane", "xqc", "hasanabi",
	"ludwig", "mizkif", "amouranth", "summit1g", "timthetatman",
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	for _, name := range creators {
		id := uuid.New()
		_, err := pool.Exec(ctx,
			`INSERT INTO creators (id, display_name) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			id, name,
		)
		if err != nil {
			log.Fatalf("insert %s: %v", name, err)
		}
		fmt.Printf("creator: %s  id: %s\n", name, id)
	}
	fmt.Printf("seeded %d creators\n", len(creators))
}
