package agent

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

const DefaultConfigTokenPath = "/etc/vetch/config-token"

// LoadConfigToken reads the private bootstrap credential, never JSON configuration.
// A missing file keeps health-only guests usable, with HTTP configuration disabled.
func LoadConfigToken(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("cannot read agent configuration credential")
	}
	token := strings.TrimSpace(string(contents))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid agent configuration credential")
	}
	return token, nil
}

func authorizedConfig(header, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(header), []byte("Bearer "+token)) == 1
}
