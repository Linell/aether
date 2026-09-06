package store

import (
	"context"
	"log"
	"time"
)

const drainBatch = 100

func RunDrain(ctx context.Context, s *Store, pub Publisher, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.DrainOutbox(ctx, pub, drainBatch); err != nil && ctx.Err() == nil {
			log.Printf("drain outbox: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.Wake():
		}
	}
}
