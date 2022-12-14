package pqmigrate

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// Config options for the library
type Config struct {
	AllInOneTx      bool      // Perform all database operations in the same transaction
	FS              *embed.FS // Support for embedded migrations in application
	BaseDirectory   string    // Directory where the sql files are stored
	DBUrl           string    // Postgresql url `psql://<user>:<pwd>@<host>:<port>/<db_name>`
	Logger          Logger    // If set the logger will be used instead of standard out
	MigrationsTable string    // Name of migrations table in database
	Debug           bool      // Show debug info
	DryRun          bool      // Perform all database operations but don't commit
}

func (c Config) filePath(fileName string) string {
	if c.FS != nil {
		return fileName
	}

	return filepath.Join(c.BaseDirectory, fileName)
}

func (c Config) fileContents(fp string) ([]byte, error) {
	if c.FS != nil {
		return c.FS.ReadFile(fp)
	}
	return os.ReadFile(fp)
}

func (c Config) readDir() ([]fs.DirEntry, error) {
	if c.FS != nil {
		return c.FS.ReadDir(".")
	}

	return os.ReadDir(c.BaseDirectory)
}
