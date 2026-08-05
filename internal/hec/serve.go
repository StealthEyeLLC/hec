package hec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tunnelclient "github.com/openai/tunnel-client"
)

func Serve(ctx context.Context) error {
	config, err := tunnelConfigFromEnvironment()
	if err != nil {
		return err
	}

	server := NewMCPServer(NewDispatcher())
	serverTransport, tunnelTransport := mcp.NewInMemoryTransports()
	serverCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(serverCtx, serverTransport)
	}()

	client, err := tunnelclient.New(config, tunnelTransport)
	if err != nil {
		return fmt.Errorf("create tunnel client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start tunnel client: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Stop(stopCtx)
	}()

	select {
	case <-client.Ready():
		fmt.Fprintln(os.Stderr, "HEC tunnel connected")
	case err := <-serverDone:
		return serverStoppedError(ctx, err)
	case <-client.Done():
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("tunnel client stopped before becoming ready")
	case <-ctx.Done():
		return nil
	}

	select {
	case <-ctx.Done():
		return nil
	case <-client.Done():
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("tunnel client stopped")
	case err := <-serverDone:
		return serverStoppedError(ctx, err)
	}
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

func serverStoppedError(ctx context.Context, err error) error {
	if ctx.Err() != nil || err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	return fmt.Errorf("MCP server stopped: %w", err)
}
