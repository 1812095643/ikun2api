package service

import "strings"

const DefaultSiteName = defaultSiteName

func siteNameOrDefault(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return DefaultSiteName
}
func paymentSiteName(cfg *PaymentConfig) string {
	if cfg == nil {
		return DefaultSiteName
	}
	return siteNameOrDefault(cfg.SiteName)
}
