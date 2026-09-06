package nats

import (
	"encoding/json"
	"fmt"

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

func (c *Client) PublishEvent(subject string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Conn.Publish(subject, data)
}
