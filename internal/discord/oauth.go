package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const tokenEndpoint = "https://discord.com/api/v10/oauth2/token"

// Token — кэш OAuth2-токена на диске (token.json).
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t *Token) valid() bool {
	return t != nil && t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-time.Minute))
}

// covers проверяет, что в кэшированном токене есть все нужные скоупы:
// после правки discord.scopes в конфиге старый токен надо перевыпустить.
func (t *Token) covers(scopes []string) bool {
	if t == nil {
		return false
	}
	have := make(map[string]bool, 8)
	for _, s := range strings.Fields(t.Scope) {
		have[s] = true
	}
	for _, want := range scopes {
		if !have[want] {
			return false
		}
	}
	return true
}

// tokenPath — абсолютный путь к кэшу токена. Config.Load заполняет TokenPath;
// откат на TokenFile нужен для клиентов, собранных без загрузки конфига.
func (c *Client) tokenPath() string {
	if c.cfg.TokenPath != "" {
		return c.cfg.TokenPath
	}
	return c.cfg.TokenFile
}

// dropToken удаляет кэш токена — вызывается, когда Discord его отверг.
func (c *Client) dropToken() {
	path := c.tokenPath()
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		c.log.Warn("не удалось удалить кэш токена", "err", err)
	}
}

// ensureToken возвращает валидный access_token:
// кэш -> refresh -> интерактивный AUTHORIZE через RPC.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	tok, err := loadToken(c.tokenPath())
	if err != nil {
		c.log.Debug("кэш токена не прочитан", "err", err)
	}
	if tok.valid() && tok.covers(c.cfg.Scopes) {
		return tok.AccessToken, nil
	}
	if tok != nil && tok.RefreshToken != "" && tok.covers(c.cfg.Scopes) {
		if fresh, err := c.exchange(ctx, url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tok.RefreshToken},
		}); err == nil {
			_ = saveToken(c.tokenPath(), fresh)
			return fresh.AccessToken, nil
		} else {
			c.log.Warn("обновление токена не удалось", "err", err)
		}
	}
	return c.authorizeInteractive(ctx)
}

// authorizeInteractive показывает пользователю окно согласия в клиенте Discord
// и обменивает полученный code на токен.
func (c *Client) authorizeInteractive(ctx context.Context) (string, error) {
	if c.cfg.ClientSecret == "" {
		return "", fmt.Errorf("%w: заполните discord.client_secret в конфиге", ErrNotAuthorized)
	}
	var resp struct {
		Code string `json:"code"`
	}
	args := map[string]any{
		"client_id": c.cfg.ClientID,
		"scopes":    c.cfg.Scopes,
	}
	// AUTHORIZE открывает диалог согласия прямо в приложении Discord.
	//
	// Если здесь придёт ошибка вида "Invalid scope" / code 4006 — значит
	// пользователь не в allowlist приложения (вкладка App Testers). Владелец
	// приложения и участники команды проходят без дополнительных действий;
	// для остальных нужен одноразовый rpc_token (см. docs/discord-integration.md).
	if err := c.call(ctx, CmdAuthorize, args, &resp); err != nil {
		return "", fmt.Errorf("authorize: %w", err)
	}
	tok, err := c.exchange(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {resp.Code},
		"redirect_uri": {c.cfg.RedirectURI},
	})
	if err != nil {
		return "", err
	}
	if err := saveToken(c.tokenPath(), tok); err != nil {
		// Не сохранили — Discord будет спрашивать согласие при каждом запуске.
		c.log.Warn("не удалось сохранить токен", "path", c.tokenPath(), "err", err)
	} else {
		c.log.Info("токен Discord сохранён", "path", c.tokenPath())
	}
	return tok.AccessToken, nil
}

// exchange выполняет POST /oauth2/token с Basic-авторизацией приложения.
func (c *Client) exchange(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ответ token endpoint: %w", err)
	}
	if body.Error != "" {
		return nil, fmt.Errorf("oauth2: %s (%s)", body.Error, body.ErrorDesc)
	}
	return &Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Scope:        body.Scope,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func loadToken(path string) (*Token, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveToken(path string, t *Token) error {
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func newNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
