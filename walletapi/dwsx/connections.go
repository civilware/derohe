package dwsx

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Connection struct {
	conn *websocket.Conn
	w    sync.Mutex
	r    sync.Mutex
}

func (c *Connection) Send(message interface{}) error {
	c.w.Lock()
	defer c.w.Unlock()
	return c.conn.WriteJSON(message)
}

func (c *Connection) Read() (int, []byte, error) {
	c.r.Lock()
	defer c.r.Unlock()
	return c.conn.ReadMessage()
}

func (c *Connection) Close() error {
	c.w.Lock()
	defer c.w.Unlock()
	return c.conn.Close()
}
