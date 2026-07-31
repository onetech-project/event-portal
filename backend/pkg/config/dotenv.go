package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

// dotEnvFile is the conventional name, resolved relative to the working
// directory the binary was started from.
const dotEnvFile = ".env"

// LoadDotEnv reads a .env file into the process environment, if one exists.
//
// It is a convenience for local development only. A variable that already has a
// non-empty value wins, so a container's injected configuration is never
// overridden by a file that happened to be baked into the image. A missing file
// is the normal production case and is not an error.
//
// A variable set to the empty string is treated as unset and IS filled in from
// the file. That matches how Load reads the environment — it also treats "" as
// absent — and avoids the trap where `FOO=` in a Compose file silently blocks the
// .env value while Load simultaneously reports FOO as missing.
//
// Call it before Load.
func LoadDotEnv() error {
	if _, err := os.Stat(dotEnvFile); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check for %s: %w", dotEnvFile, err)
	}

	// Read rather than Load: applying the values by hand is what lets the
	// empty-is-unset rule above hold.
	values, err := godotenv.Read(dotEnvFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", dotEnvFile, err)
	}

	for key, value := range values {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("apply %s from %s: %w", key, dotEnvFile, err)
		}
	}
	return nil
}
