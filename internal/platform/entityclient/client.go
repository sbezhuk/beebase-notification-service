package entityclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

type Client struct {
	urls  map[reminder.EntityType]string
	token string
	http  *http.Client
}

func New(urls map[reminder.EntityType]string, token string) *Client {
	return &Client{urls: urls, token: token, http: &http.Client{Timeout: 5 * time.Second}}
}
func (c *Client) Exists(ctx context.Context, t reminder.EntityType, id uuid.UUID) (bool, error) {
	base := c.urls[t]
	if base == "" {
		return false, fmt.Errorf("entity service unavailable")
	}
	plural := map[reminder.EntityType]string{reminder.EntityApiary: "apiaries", reminder.EntityHive: "hives", reminder.EntityInspection: "inspections", reminder.EntityHarvest: "harvests"}[t]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/internal/api/v1/%s/%s/exists", base, plural, id), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("entity service returned %d", resp.StatusCode)
	}
}
