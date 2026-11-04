package config

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Enabled        bool     `json:"enabled"`
	IdleSeconds    int      `json:"idle_seconds"`
	MaxSeconds     int      `json:"max_seconds"`
	MaxPerUser     int      `json:"max_per_user"`
	MaxTotal       int      `json:"max_total"`
	MinUID         uint32   `json:"min_uid"`
	AllowedUsers   []string `json:"allowed_users"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func Defaults() Config {
	return Config{IdleSeconds: 900, MaxSeconds: 3600, MaxPerUser: 4, MaxTotal: 32, MinUID: 1000}
}
func Parse(r io.Reader) (Config, error) {
	c := Defaults()
	d := json.NewDecoder(io.LimitReader(r, 65537))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("extra configuration data")
	}
	if c.IdleSeconds < 1 || c.IdleSeconds > 86400 || c.MaxSeconds < 1 || c.MaxSeconds > 86400 || c.MaxPerUser < 1 || c.MaxPerUser > 32 || c.MaxTotal < c.MaxPerUser || c.MaxTotal > 256 || c.MinUID < 1000 {
		return c, errors.New("invalid configuration limits")
	}
	for _, s := range c.AllowedOrigins {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(s, "*") {
			return c, errors.New("origins must be exact http(s) origins")
		}
	}
	return c, nil
}
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return Parse(f)
}
