// Package mtproto provides a high-level client for interacting with Telegram's MTProto API
package mtproto

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/celestix/gotgproto"
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/sessionMaker"
	"github.com/sanity-io/litter"
	"golang.org/x/exp/slog"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Common errors returned by the client
var (
	ErrInvalidConfig    = errors.New("invalid configuration")
	ErrClientNotStarted = errors.New("client not started")
	ErrChatNotFound     = errors.New("chat not found")
	ErrNotInitialized   = errors.New("client not initialized")
	ErrRateLimit        = errors.New("rate limit exceeded")
)

// ClientType represents the type of Telegram client (bot or user)
type ClientType int

const (
	ClientTypeUser ClientType = iota
	ClientTypeBot
)

// Config holds the configuration for the Telegram client
type Config struct {
	// Required fields
	AppID   int    `json:"app_id" yaml:"app_id"`
	APIHash string `json:"api_hash" yaml:"api_hash"`

	// Authentication (either Phone or BotToken is required)
	Phone string `json:"phone,omitempty" yaml:"phone,omitempty"`

	// Database configuration
	DatabaseConfig DatabaseConfig `json:"database" yaml:"database"`

	NoAutoAuth bool `json:"no_auto_auth" yaml:"no_auto_auth"`

	NoBlockInit bool `json:"no_block_init" yaml:"no_block_init"`

	AuthConversator gotgproto.AuthConversator
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type        string `json:"type" yaml:"type"` // "postgres" or "sqlite"
	DSN         string `json:"dsn" yaml:"dsn"`
	MaxConns    int    `json:"max_conns" yaml:"max_conns"`
	TablePrefix string `json:"table_prefix" yaml:"table_prefix"`
}

// RateLimitConfig defines rate limiting parameters
type RateLimitConfig struct {
	MessagesPerMinute int `json:"messages_per_minute" yaml:"messages_per_minute"`
	RequestsPerMinute int `json:"requests_per_minute" yaml:"requests_per_minute"`
}

// Client represents a Telegram MTProto client
type Client struct {
	cfg    *Config
	logger *slog.Logger

	client     *gotgproto.Client
	dispatcher dispatcher.Dispatcher
	db         *gorm.DB

	handlers []UpdateHandler

	ctx    context.Context
	cancel context.CancelFunc

	started bool
	mu      sync.RWMutex
}

// NewClient creates a new Telegram client with the given configuration
func NewClient(logger *slog.Logger, cfg *Config) (*Client, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		cfg:      cfg,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		handlers: make([]UpdateHandler, 0),
	}

	if cfg.NoBlockInit {
		if err := client.initialize(cfg); err != nil {
			return client, fmt.Errorf("initialization failed: %w", err)
		}
	} else {
		go func() {
			if err := client.initialize(cfg); err != nil {
				logger.Error("initialization failed", slog.String("err", err.Error()))
			}
		}()
	}

	return client, nil
}

// Initialize sets up the client's dependencies
func (c *Client) initialize(cfg *Config) error {
	// Initialize database
	db, err := c.setupDatabase()
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}

	c.db = db

	// Setup client options
	opts := &gotgproto.ClientOpts{
		Session:          sessionMaker.SqlSession(db.Dialector),
		SystemLangCode:   "en",
		ClientLangCode:   "en",
		DisableCopyright: true,
		NoAutoAuth:       cfg.NoAutoAuth,
		AuthConversator:  cfg.AuthConversator,
	}

	// Create Telegram client
	client, err := gotgproto.NewClient(
		c.cfg.AppID,
		c.cfg.APIHash,
		gotgproto.ClientTypePhone(c.cfg.Phone),
		opts,
	)

	c.client = client
	c.dispatcher = client.Dispatcher

	return err
}

// Stop gracefully stops the client
func (c *Client) Stop() error {
	c.cancel()

	return nil
}

// ChannelMembersOptions contains options for fetching channel members
type ChannelMembersOptions struct {
	MaxPages   int
	MaxUsers   int
	Offset     int
	Filter     string
	ActiveOnly bool
	RetryCount int
	RetryDelay time.Duration
}

type HandlerFunc func(ctx *ext.Context, update *ext.Update) error

func (f HandlerFunc) CheckUpdate(ctx *ext.Context, update *ext.Update) error {
	return f(ctx, update)
}

// UpdateHandler interface for processing updates
type UpdateHandler interface {
	HandleUpdate(ctx *ext.Context, update *ext.Update) error
}

// AddHandler adds an update handler to the client
func (c *Client) AddHandler(handler UpdateHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers = append(c.handlers, handler)

	c.client.Dispatcher.AddHandler(HandlerFunc(handler.HandleUpdate))
}

// Helper functions
func (c *Client) setupDatabase() (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch c.cfg.DatabaseConfig.Type {
	case "postgres":
		dialector = postgres.Open(c.cfg.DatabaseConfig.DSN)
	default:
		dialector = sqlite.Open(c.cfg.DatabaseConfig.DSN)
	}

	gormConfig := &gorm.Config{}

	if c.cfg.DatabaseConfig.TablePrefix != "" {
		fmt.Println("Setting table prefix", c.cfg.DatabaseConfig.TablePrefix)
		gormConfig.NamingStrategy = schema.NamingStrategy{
			TablePrefix: c.cfg.DatabaseConfig.TablePrefix,
		}
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database instance: %w", err)
	}

	sqlDB.SetMaxOpenConns(c.cfg.DatabaseConfig.MaxConns)
	sqlDB.SetMaxIdleConns(c.cfg.DatabaseConfig.MaxConns / 2)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	if cfg.AppID == 0 {
		return errors.New("app_id is required")
	}

	if cfg.APIHash == "" {
		return errors.New("api_hash is required")
	}

	if cfg.Phone == "" {
		return errors.New("phone is required")
	}

	return nil
}

func (s *Client) IsLoggedIn() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := s.client.Auth().Status(ctx)
	if err != nil {
		return false, fmt.Errorf("get status: %w", err)
	}

	if status.Authorized {
		litter.Dump(status)
	}

	return status.Authorized, nil
}

func (s *Client) WaitUntilLoggedIn() (bool, error) {
	timeout := time.After(time.Minute)

	for {
		select {
		case <-timeout:
			return false, fmt.Errorf("timed out waiting for login")
		case <-time.After(2 * time.Second):
			loggedIn, err := s.IsLoggedIn()
			if err != nil {
				return false, fmt.Errorf("check logged in: %w", err)
			}

			if loggedIn {
				return true, nil
			}
		}
	}
}
