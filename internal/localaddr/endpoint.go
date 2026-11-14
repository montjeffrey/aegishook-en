package localaddr

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint permits HTTPS services and HTTP over a local loopback/tunnel only.
func Endpoint(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("review service address must be an HTTPS origin (no credentials, path, query or fragment); HTTP is allowed only for a loopback address")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && IsLoopback(u.Hostname())) {
		return "", errors.New("remote review service address must use HTTPS; HTTP is allowed only for a loopback address")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("review service address port is invalid")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}
