package store

import (
	"context"
	"log"
	"time"
)

const DrainBatch = 100

func RunDrain(ctx context.Context, s *Store, pub Publisher, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		drainOnce(ctx, s, pub)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.Wake():
		}
	}
}

func drainOnce(ctx context.Context, s *Store, pub Publisher) {
	if _, err := s.DrainOutbox(ctx, pub, DrainBatch); err != nil && ctx.Err() == nil {
		log.Printf("drain outbox: %v", err)
	}
}
