package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "strings"
    "time"
)

func main() {
    var sub, role, secret, expStr string

    flag.StringVar(&sub, "sub", "user@example.com", "subject/user")
    flag.StringVar(&role, "role", "admin", "role: viewer|operator|admin")
    flag.StringVar(&secret, "secret", "", "HS256 secret (required)")
    flag.StringVar(&expStr, "exp", "1h", "expiration (e.g., 1h, 30m, 0s for no exp)")
    flag.Parse()

    if secret == "" {
        fmt.Fprintln(os.Stderr, "secret is required")
        os.Exit(1)
    }

    header := map[string]string{"alg": "HS256", "typ": "JWT"}
    hdrB, _ := json.Marshal(header)

    claims := map[string]any{"sub": sub, "role": role}
    if expStr != "0s" {
        d, err := time.ParseDuration(expStr)

        if err != nil {
            fmt.Fprintln(os.Stderr, "bad exp:", err)
            os.Exit(1)
        }

        claims["exp"] = time.Now().Add(d).Unix()
    }

    plB, _ := json.Marshal(claims)

    enc := base64.RawURLEncoding
    part0 := enc.EncodeToString(hdrB)
    part1 := enc.EncodeToString(plB)

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(part0))
    mac.Write([]byte("."))
    mac.Write([]byte(part1))

    sig := enc.EncodeToString(mac.Sum(nil))

    fmt.Println(strings.Join([]string{part0, part1, sig}, "."))
}
