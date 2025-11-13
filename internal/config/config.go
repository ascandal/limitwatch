package config

import (
    "os"
    "strings"
)

// Getenv returns the value of the environment variable named by the key,
// or fallback if the variable is not present or empty.
func Getenv(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }

    return fallback
}

// BoolEnv parses a boolean environment variable with common true/false values.
func BoolEnv(key string, def bool) bool {
    if v, ok := os.LookupEnv(key); ok {
        switch strings.ToLower(strings.TrimSpace(v)) {
        case "1", "t", "true", "y", "yes", "on":
            return true
        case "0", "f", "false", "n", "no", "off":
            return false
        }
    }

    return def
}
