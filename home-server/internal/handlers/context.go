package handlers

import (
	"context"
	"time"
)

// contextWithTimeout wraps context.WithTimeout for use in handlers
// without requiring context imports in each handler file.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
