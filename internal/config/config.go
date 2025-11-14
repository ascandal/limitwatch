package config

import (
    "os"
    "strconv"
    "strings"
)

// Returns the value of the environment variable named by the key,
// with fallback if not present or empty.
func Getenv(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }

    return fallback
}

// Parses an integer environment variable with a default.
func GetenvInt(key string, def int) int {
    if v := os.Getenv(key); v != "" {
        if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
            return n
        }
    }

    return def
}

// Parses a boolean environment variable with common true/false values.
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
