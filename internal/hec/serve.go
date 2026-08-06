package hec

import (
	"context"
	"errors"
	"os"
	"strings"

	tunnelclient "github.com/openai/tunnel-client"
)

func Serve(ctx context.Context) error {
	config, err := tunnelConfigFromEnvironment()
	if err != nil {
		return err
	}
	return newGenerationManager(config, NewDispatcher(), nil).Run(ctx)
}

func tunnelConfigFromEnvironment() (tunnelclient.Config, error) {
	tunnelID := strings.TrimSpace(os.Getenv("CONTROL_PLANE_TUNNEL_ID"))
	if tunnelID == "" {
		return tunnelclient.Config{}, errors.New("CONTROL_PLANE_TUNNEL_ID is required")
	}
	apiKey := strings.TrimSpace(os.Getenv("CONTROL_PLANE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return tunnelclient.Config{}, errors.New("CONTROL_PLANE_API_KEY or OPENAI_API_KEY is required")
	}
	return tunnelclient.Config{
		TunnelID:            tunnelID,
		APIKey:              apiKey,
		ControlPlaneBaseURL: strings.TrimSpace(os.Getenv("CONTROL_PLANE_BASE_URL")),
		OrganizationID:      strings.TrimSpace(os.Getenv("CONTROL_PLANE_ORGANIZATION_ID")),
	}, nil
}
