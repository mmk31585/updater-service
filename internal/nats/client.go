package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

type Config struct {
	URL             string
	ConnectTimeout  time.Duration
	ReconnectPeriod time.Duration
	MaxReconnect    int
	DrainTimeout    time.Duration
}

type Client struct {
	conn   *nats.Conn
	logger *slog.Logger
	cfg    Config
}

func New(cfg Config, logger *slog.Logger) (*Client, error) {
	client := &Client{
		logger: logger,
		cfg:    cfg,
	}
	if err := client.connect(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) connect() error {
	conn, err := nats.Connect(
		c.cfg.URL,
		nats.Timeout(c.cfg.ConnectTimeout),
		nats.ReconnectWait(c.cfg.ReconnectPeriod),
		nats.MaxReconnects(c.cfg.MaxReconnect),
		nats.DrainTimeout(c.cfg.DrainTimeout),
	)
	if err != nil {
		return err
	}

	c.conn = conn
	c.logger.Info("nats connected", "url", c.cfg.URL)
	return nil
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.logger.Info("nats disconnected")
	}
}

func (c *Client) Subscribe(
	subject string,
	handler func(*nats.Msg),
) (*nats.Subscription, error) {
	sub, err := c.conn.Subscribe(subject, handler)
	if err != nil {
		c.logger.Error("failed to subscribe", "subject", subject, "error", err)
		return nil, err
	}
	c.logger.Info("subscribed", "subject", subject)
	return sub, nil
}

func (c *Client) Publish(subject string, data []byte) error {
	if err := c.conn.Publish(subject, data); err != nil {
		c.logger.Error("failed to publish", "subject", subject, "error", err)
		return err
	}
	return nil
}

func (c *Client) Flush() error {
	if err := c.conn.Flush(); err != nil {
		c.logger.Error("failed to flush nats", "error", err)
		return err
	}
	return nil
}

func (c *Client) PublishAndFlush(subject string, data []byte) error {
	if err := c.Publish(subject, data); err != nil {
		return err
	}
	return c.Flush()
}

func (c *Client) Connection() *nats.Conn {
	return c.conn
}

func (c *Client) Run(ctx context.Context) {
	<-ctx.Done()
	c.Close()
}
