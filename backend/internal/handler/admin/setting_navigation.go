package admin

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/config"
)

func validateCustomPageURL(raw string) error {
	if !strings.HasPrefix(raw, "/") {
		return config.ValidateAbsoluteHTTPURL(raw)
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host != "" || u.User != nil || u.IsAbs() || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n\t") {
		return fmt.Errorf("invalid local page URL")
	}
	return nil
}
