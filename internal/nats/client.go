package nats

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type Client struct {
	Conn *nats.Conn
}

func Connect(url string) (*Client, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return &Client{Conn: conn}, nil
}

func (c *Client) Close() {
	c.Conn.Close()
}

func (c *Client) Drain(timeout time.Duration) error {
	closed := make(chan struct{})
	c.Conn.SetClosedHandler(func(*nats.Conn) { close(closed) })
	if err := c.Conn.Drain(); err != nil {
		return err
	}
	select {
	case <-closed:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("drain: not finished in %s", timeout)
	}
}

func (c *Client) PublishEvent(subject string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Conn.Publish(subject, data)
}
