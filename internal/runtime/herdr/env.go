package herdr

import (
	"os"
	"strings"
)

func scrubEnvironment(environ []string, socketPath string) []string {
	clean := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "HERDR_") {
			continue
		}
		clean = append(clean, entry)
	}
	if socketPath != "" {
		clean = append(clean, "HERDR_SOCKET_PATH="+socketPath)
	}
	return clean
}

func currentChildEnvironment(socketPath string) []string {
	return scrubEnvironment(os.Environ(), socketPath)
}
