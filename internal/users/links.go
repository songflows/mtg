package users

import (
	"net"
	"net/url"
	"fmt"

	"github.com/9seconds/mtg/v2/internal/utils"
	"github.com/9seconds/mtg/v2/mtglib"
)

// LinkSettings controls public endpoints in generated tg:// links.
type LinkSettings struct {
	PublicHost string
	PublicPort uint
}

// UserLinks matches telemt UserLinks (mtg supports ee/tls mode only).
type UserLinks struct {
	TLS []string `json:"tls"`
}

// BuildUserLinks builds tg:// and t.me proxy URLs for a user secret.
func BuildUserLinks(settings LinkSettings, secret mtglib.Secret, hexEncoding bool) UserLinks {
	hosts := linkHosts(settings)
	links := make([]string, 0, len(hosts))

	for _, host := range hosts {
		links = append(links, buildProxyURL(host, settings.PublicPort, secret, hexEncoding))
	}

	return UserLinks{TLS: links}
}

func linkHosts(settings LinkSettings) []string {
	if settings.PublicHost != "" {
		return []string{settings.PublicHost}
	}

	return nil
}

func buildProxyURL(host string, port uint, secret mtglib.Secret, hexEncoding bool) string {
    // Preserve parameter order: server, port, secret
    var secretStr string
    if hexEncoding {
        secretStr = secret.Hex()
    } else {
        secretStr = secret.Base64()
    }
    // Manually encode parameters to keep order
    rawQuery := fmt.Sprintf("server=%s&port=%d&secret=%s", url.QueryEscape(host), port, url.QueryEscape(secretStr))
    return (&url.URL{
        Scheme:   "tg",
        Host:     "proxy",
        RawQuery: rawQuery,
    }).String()
}

// LinkSettingsFromIPs merges explicit settings with resolved public IPs.
func LinkSettingsFromIPs(settings LinkSettings, ipv4, ipv6 net.IP, bindPort uint) LinkSettings {
	rv := settings

	if rv.PublicPort == 0 {
		rv.PublicPort = bindPort
	}

	if rv.PublicHost != "" {
		return rv
	}

	if ipv4 != nil {
		rv.PublicHost = ipv4.String()

		return rv
	}

	if ipv6 != nil {
		rv.PublicHost = ipv6.String()
	}

	return rv
}

// TmeURL converts tg://proxy link to https://t.me/proxy.
func TmeURL(tgURL string) (string, error) {
	parsed, err := url.Parse(tgURL)
	if err != nil {
		return "", err
	}

	return (&url.URL{
		Scheme:   "https",
		Host:     "t.me",
		Path:     "proxy",
		RawQuery: parsed.RawQuery,
	}).String(), nil
}

// QRCodeURL wraps a link as QR image URL (same helper as CLI access).
func QRCodeURL(link string) string {
	return utils.MakeQRCodeURL(link)
}
