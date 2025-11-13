package storage

// _ "modernc.org/sqlite" imports the package only for its side-effects
// It will not reference any of its exported names directly.
// `sqlite.` cannot be referenced.
// Used so sql.Open("sqlite", dsn) succeeds without error “unknown driver ‘sqlite’”.
import (
    "context"
    "database/sql"
    _ "modernc.org/sqlite"
    "embed"
    "fmt"
    "io/fs"
    "sort"
    "strconv"
    "strings"
)

// Opens a database connection using database/sql.
// For SQLite, pass a DSN like:
//  - file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)
func Open(dsn string) (*sql.DB, error) {
    // If DSN starts with "file:" assume sqlite.
    driver := "sqlite"

    if strings.HasPrefix(dsn, "file:") || strings.HasSuffix(strings.ToLower(dsn), ".db") {
        driver = "sqlite"
    }

    db, err := sql.Open(driver, dsn)

    if err != nil {
        return nil, err
    }

    if err := db.Ping(); err != nil {
        _ = db.Close(); return nil, err
    }

    return db, nil
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Applies embedded SQL migrations in numeric order (e.g., 0001_init.sql).
func Migrate(ctx context.Context, db *sql.DB) error {
    if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
        return fmt.Errorf("create schema_migrations: %w", err)
    }

    applied := map[int]bool{}
    rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)

    if err != nil {
        return fmt.Errorf("read schema_migrations: %w", err)
    }

    for rows.Next() {
        var v int

        if err := rows.Scan(&v); err != nil {
            return err
        }

        applied[v] = true
    }

    _ = rows.Close()

    entries, err := fs.ReadDir(migrationsFS, "migrations")

    if err != nil {
        return err
    }

    type mig struct{ v int; name string }
    var list []mig

    for _, e := range entries {
        name := e.Name()

        if !strings.HasSuffix(name, ".sql") {
            continue
        }

        // Expect prefix like 0001_*
        n := strings.SplitN(name, "_", 2)[0]
        v, err := strconv.Atoi(strings.TrimLeft(n, "0"))

        if err != nil {
            // Handle "0000" -> 0 case
            if n == "0000" {
                v = 0
            } else {
                return fmt.Errorf("bad migration filename: %s", name)
            }
        }

        list = append(list, mig{v: v, name: name})
    }

    sort.Slice(list, func(i, j int) bool { return list[i].v < list[j].v })

    for _, m := range list {
        if applied[m.v] {
            continue
        }

        sqlb, err := migrationsFS.ReadFile("migrations/" + m.name)

        if err != nil {
            return err
        }

        if _, err := db.ExecContext(ctx, string(sqlb)); err != nil {
            return fmt.Errorf("apply migration %s: %w", m.name, err)
        }

        if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, m.v); err != nil {
            return fmt.Errorf("record migration %d: %w", m.v, err)
        }
    }

    return nil
}
