package telegram

import (
	"fmt"
	"log/slog"

	"tgproxy/config"
	"tgproxy/router"

	"github.com/zelenin/go-tdlib/client"
)

type Client struct {
	cfg       *config.Config
	router    *router.Router
	log       *slog.Logger
	client    *client.Client
	authState string
}

func NewClient(cfg *config.Config, router *router.Router, log *slog.Logger) (*Client, error) {
	params := &client.SetTdlibParametersRequest{
		DatabaseDirectory:  cfg.TDLIBDatabaseDir,
		UseMessageDatabase: true,
		UseSecretChats:     false,
		ApiId:              cfg.APIID,
		ApiHash:            cfg.APIHash,
		SystemLanguageCode: "en",
		DeviceModel:        "Server",
		SystemVersion:      "1.0.0",
		ApplicationVersion: "1.0.0",
	}

	authorizer := client.ClientAuthorizer(params)

	_, err := client.SetLogVerbosityLevel(&client.SetLogVerbosityLevelRequest{
		NewVerbosityLevel: cfg.TDLIBLogLevel,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create TDLib client: %w", err)
	}

	go client.CliInteractor(authorizer)

	// Create TDLib client
	tdlibClient, err := client.NewClient(authorizer)

	if err != nil {
		return nil, fmt.Errorf("failed to create TDLib client: %w", err)
	}

	log.Info("TDLib client created", "database_dir", cfg.TDLIBDatabaseDir)

	return &Client{
		cfg:    cfg,
		router: router,
		log:    log,
		client: tdlibClient,
	}, nil
}

func (c *Client) Close() {
	if c.client != nil {
		c.client.Close()
		c.log.Info("TDLib client closed")
	}
}

func (c *Client) GetRawClient() *client.Client {
	return c.client
}
