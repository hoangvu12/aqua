package riot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrLockfileNotFound means VALORANT's Riot Client isn't running (no lockfile).
var ErrLockfileNotFound = errors.New("riot: lockfile not found")

// Lockfile holds the bits Aqua needs from the Riot Client lockfile:
// %LOCALAPPDATA%\Riot Games\Riot Client\Config\lockfile, which is
// colon-separated as name:pid:port:password:protocol.
type Lockfile struct {
	Port     string
	Password string
}

// ReadLockfile reads and parses the lockfile. Riot holds it open with shared
// read/write access, and Go opens files with FILE_SHARE_READ|WRITE, so a plain
// read succeeds while the client is running.
func ReadLockfile() (*Lockfile, error) {
	path := filepath.Join(os.Getenv("LOCALAPPDATA"), "Riot Games", "Riot Client", "Config", "lockfile")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrLockfileNotFound
	}
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) < 5 {
		return nil, errors.New("riot: malformed lockfile")
	}
	return &Lockfile{Port: parts[2], Password: parts[3]}, nil
}
