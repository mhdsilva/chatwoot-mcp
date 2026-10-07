package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"chatwoot-mcp/internal/core"
)

const (
	configDirName  = "chatwoot-mcp"
	configFileName = "config.json"

	configFileMode = 0o600
	configDirMode  = 0o700
)

// store persists credentials as a single JSON file with restrictive permissions.
type store struct {
	path string
}

// NewStore returns a core.Store backed by path. An empty path selects the
// per-user default os.UserConfigDir()/chatwoot-mcp/config.json.
func NewStore(path string) core.Store {
	if strings.TrimSpace(path) == "" {
		path = defaultPath()
	}
	return &store{path: path}
}

func defaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, configDirName, configFileName)
}

// diskSettings is the on-disk shape. core.Settings omits Token from JSON on
// purpose, so persistence needs its own representation.
type diskSettings struct {
	BaseURL   string `json:"base_url"`
	AccountID int64  `json:"account_id"`
	Token     string `json:"token"`
}

func (s *store) Load(ctx context.Context) (core.Settings, error) {
	if err := ctx.Err(); err != nil {
		return core.Settings{}, err
	}

	info, err := os.Lstat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return core.Settings{}, nil
	}
	if err != nil {
		return core.Settings{}, fmt.Errorf("inspect config file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return core.Settings{}, errors.New("config file must not be a symlink")
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return core.Settings{}, fmt.Errorf("read config file: %w", err)
	}

	var disk diskSettings
	if err := json.Unmarshal(data, &disk); err != nil {
		return core.Settings{}, fmt.Errorf("decode config file: %w", err)
	}

	return core.Settings{
		BaseURL:   disk.BaseURL,
		AccountID: disk.AccountID,
		Token:     disk.Token,
	}, nil
}

func (s *store) Save(ctx context.Context, settings core.Settings) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(settings); err != nil {
		return err
	}
	if err := rejectSymlink(s.path); err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(diskSettings{
		BaseURL:   settings.BaseURL,
		AccountID: settings.AccountID,
		Token:     settings.Token,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(configFileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restrict temporary config file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary config file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect config file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("config file must not be a symlink")
	}
	return nil
}

func validate(settings core.Settings) error {
	if strings.TrimSpace(settings.Token) == "" {
		return errors.New("token is required")
	}
	if settings.AccountID <= 0 {
		return errors.New("account ID must be a positive integer")
	}
	return validateBaseURL(settings.BaseURL)
}

func validateBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("base URL is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("base URL is invalid")
	}
	if parsed.Host == "" {
		return errors.New("base URL must include a host")
	}

	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return errors.New("base URL must use https; http is allowed only for loopback development hosts")
	default:
		return errors.New("base URL must use https")
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
