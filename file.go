package pqmigrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func (ctx *PQMigrate) fileEnsureDirExist(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		ctx.dbg("fileEnsureDirExists", fmt.Sprintf("directory '%s' does not exist, creating", path))
		if err := os.MkdirAll(path, 0755); err != nil {
			ctx.dbg("fileEnsureExists", err)
			return err
		}
	}
	return nil
}

func (ctx *PQMigrate) fileGetContents(fileName string) (string, error) {
	fp := ctx.config.filePath(fileName)
	ctx.dbgJoin("fileGetContents", "getting:", fp)
	cb, err := ctx.config.fileContents(fp)
	if err != nil {
		ctx.dbg("fileGetContents", err)
		return "", err
	}
	return norm.NFC.String(string(cb)), nil
}

func (ctx *PQMigrate) fileGetContentsTrimmed(fileName string) (string, error) {
	fc, err := ctx.fileGetContents(fileName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(fc), nil
}

func (ctx *PQMigrate) fileWriteContents(fileName string, contents []byte) error {
	fp := filepath.Join(ctx.config.BaseDirectory, fileName)
	ctx.dbg("fileWriteContents", fp)
	if err := ctx.fileEnsureDirExist(ctx.config.BaseDirectory); err != nil {
		return err
	}
	return os.WriteFile(fp, contents, 0644)
}

func (ctx *PQMigrate) fileExec(fileName string) error {
	ctx.dbg("fileExec", fileName)
	contents, err := ctx.fileGetContentsTrimmed(fileName)
	if err != nil {
		ctx.dbg("fileExec", err)
		return err
	}
	return ctx.dbExecString(contents, nil)
}

func (ctx *PQMigrate) fileRemove(fileName string) error {
	ctx.dbg("fileRemove", fileName)
	fp := filepath.Join(ctx.config.BaseDirectory, fileName)
	if err := os.Remove(fp); err != nil {
		ctx.dbg("fileRemove", err)
		return err
	}
	return nil
}
