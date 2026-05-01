package ports

import (
	"context"
	"net/http"

	"goproxy/internal/domain"
)

// GeoIPResolver resolves a validated proxy client's exit IP and location data.
type GeoIPResolver interface {
	Resolve(ctx context.Context, client *http.Client) (string, string, domain.IPInfo)
}
