// Package hyperhdr — клиент JSON-API HyperHDR (TCP-порт 19444, по строке JSON на запрос).
package hyperhdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// Client — ленивое TCP-соединение с автопереподключением.
type Client struct {
	cfg config.HyperHDR
	log *slog.Logger

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

func New(cfg config.HyperHDR, log *slog.Logger) *Client {
	return &Client{cfg: cfg, log: log}
}

func (c *Client) addr() string {
	return fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.JSONPort)
}

// connect устанавливает соединение и при необходимости логинится токеном.
func (c *Client) connect(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.addr())
	if err != nil {
		return fmt.Errorf("подключение к HyperHDR %s: %w", c.addr(), err)
	}
	c.conn = conn
	c.rd = bufio.NewReader(conn)

	if c.cfg.Token != "" {
		if _, err := c.sendLocked(Request{
			Command:    "authorize",
			Subcommand: "login",
			Token:      c.cfg.Token,
		}); err != nil {
			c.dropLocked()
			return fmt.Errorf("авторизация в HyperHDR: %w", err)
		}
	}
	c.log.Info("подключено к HyperHDR", "addr", c.addr())
	return nil
}

func (c *Client) dropLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn, c.rd = nil, nil
}

// send отправляет запрос с одной попыткой переподключения.
func (c *Client) send(ctx context.Context, req Request) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for attempt := 0; attempt < 2; attempt++ {
		if err := c.connect(ctx); err != nil {
			return nil, err
		}
		res, err := c.sendLocked(req)
		if err == nil {
			return res, nil
		}
		c.log.Debug("повтор запроса к HyperHDR", "err", err)
		c.dropLocked()
	}
	return nil, fmt.Errorf("HyperHDR недоступен: %s", c.addr())
}

// sendLocked пишет JSON-строку и читает одну строку ответа. Требует c.mu.
func (c *Client) sendLocked(req Request) (*Response, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write(append(raw, '\n')); err != nil {
		return nil, err
	}
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var res Response
	if err := json.Unmarshal(line, &res); err != nil {
		return nil, fmt.Errorf("разбор ответа HyperHDR: %w", err)
	}
	if !res.Success {
		return &res, fmt.Errorf("HyperHDR отклонил команду %q: %s", req.Command, res.Error)
	}
	return &res, nil
}

// SetColor заливает ленту сплошным цветом на нашем приоритете.
// durationMS == 0 — до следующей команды.
func (c *Client) SetColor(ctx context.Context, r, g, b, durationMS int) error {
	_, err := c.send(ctx, Request{
		Command:  "color",
		Color:    []int{r, g, b},
		Priority: c.cfg.Priority,
		Origin:   c.cfg.Origin,
		Duration: durationMS,
	})
	return err
}

// SetEffect запускает встроенный эффект HyperHDR по имени.
func (c *Client) SetEffect(ctx context.Context, name string, durationMS int) error {
	_, err := c.send(ctx, Request{
		Command:  "effect",
		Effect:   &Effect{Name: name},
		Priority: c.cfg.Priority,
		Origin:   c.cfg.Origin,
		Duration: durationMS,
	})
	return err
}

// Clear снимает наш приоритет — подсветка возвращается к обычному источнику.
func (c *Client) Clear(ctx context.Context) error {
	_, err := c.send(ctx, Request{Command: "clear", Priority: c.cfg.Priority})
	return err
}

// ServerInfo — для проверки связи и списка эффектов в веб-панели.
func (c *Client) ServerInfo(ctx context.Context) (json.RawMessage, error) {
	res, err := c.send(ctx, Request{Command: "serverinfo"})
	if err != nil {
		return nil, err
	}
	return res.Info, nil
}

// Close закрывает соединение, при необходимости сняв приоритет.
func (c *Client) Close(ctx context.Context) error {
	if c.cfg.RestoreOnExit {
		if err := c.Clear(ctx); err != nil {
			c.log.Warn("не удалось снять приоритет при выходе", "err", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
	return nil
}
