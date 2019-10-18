package pgmigrate

import (
	"fmt"
	"github.com/gocraft/dbr"
	_ "github.com/lib/pq"
)

const dbTableSchema = `create table if not exists %s (
			version bigint not null primary key,
			name text not null default '',
			up text not null default '',
			down text not null default ''
		)`

type execCB func(tx *dbr.Tx) error

func (ctx *PGMigrate) dbConnectWithURL(url string) (*dbr.Connection, error) {
	ctx.dbg("dbConnectWithURL")
	return dbr.Open("postgres", url, nil)
}

func (ctx *PGMigrate) dbConnect() (*dbr.Connection, error) {
	ctx.dbg("dbConnect")
	if ctx.dbConn != nil {
		ctx.dbg("dbConnect", "Database connection already established")
		return ctx.dbConn, nil
	}
	var err error
	ctx.dbConn, err = ctx.dbConnectWithURL(ctx.config.DBUrl)
	return ctx.dbConn, err
}

func (ctx *PGMigrate) dbGetConn() (*dbr.Connection, error) {
	ctx.dbg("dbGetConn")
	return ctx.dbConnect()
}

func (ctx *PGMigrate) dbGetTx() (*dbr.Tx, error) {
	ctx.dbg("dbGetTx")
	if ctx.tx != nil {
		ctx.dbg("dbGetTx", "transaction already active")
		return ctx.tx, nil
	}
	conn, err := ctx.dbGetConn()
	if err != nil {
		ctx.dbg("dbGetTx", err)
		return nil, err
	}
	session := conn.NewSession(nil)
	_tx, err := session.Begin()
	if err != nil {
		ctx.dbg("dbGetTx", err)
		return nil, err
	}
	ctx.tx = _tx
	return ctx.tx, nil
}

func (ctx *PGMigrate) dbTokensToURL() string {
	urlFormat := "postgres://%s:%s@%s:%s"
	gtd := func(tokenName, def string) string {
		token, found := ctx.dbTokens[tokenName]
		if !found {
			return def
		}
		return token
	}
	return fmt.Sprintf(urlFormat, gtd("user", ""), gtd("password", ""), gtd("host", "localhost"), gtd("port", "5432"))
}

func (ctx *PGMigrate) dbExists(session *dbr.Session, datname string) (bool, error) {
	dbExists := false
	if err := session.SelectBySql("select exists(select 1 from pg_database where datname = ?)", datname).LoadOne(&dbExists); err != nil {
		ctx.dbg("dbExists", err)
		return false, err
	}
	return dbExists, nil
}

func (ctx *PGMigrate) dbEnsureDBExists(cb ConfirmCB) error {
	url := ctx.dbTokensToURL()
	ctx.dbgJoin("InitDB", "initing db at:", url)
	dbConn, err := ctx.dbConnectWithURL(url)
	if err != nil {
		ctx.dbg("dbEnsureDBExists", err)
		return err
	}
	defer dbConn.Close()
	session := dbConn.NewSession(nil)
	datname, found := ctx.dbTokens["dbname"]
	if !found {
		ctx.dbg("dbEnsureDBExists", "empty database name")
		return fmt.Errorf("empty database name")
	}
	if dbExists, err := ctx.dbExists(session, datname); err == nil {
		if dbExists {
			ctx.logger.Inf("database '%s' already exists", datname)
			return nil
		}
	} else {
		return err
	}
	if cb != nil && !cb(fmt.Sprintf("create db: '%s'?", datname)) {
		ctx.dbg("dbEnsureDBExists", "callback and false return")
		ctx.logger.Warn("aborting...")
		return nil
	}
	ctx.logger.Inf(fmt.Sprintf("creating database %s", datname))
	if _, err := session.Exec(fmt.Sprintf("create database %s;", datname)); err != nil {
		ctx.dbg("dbEnsureDBExists", err)
		return err
	}
	ctx.logger.Ok(fmt.Sprintf("database '%s' created", datname))
	return nil
}

func (ctx *PGMigrate) dbDropDB(cb ConfirmCB) error {
	url := ctx.dbTokensToURL()
	dbConn, err := ctx.dbConnectWithURL(url)
	if err != nil {
		ctx.dbg("dbDropDB", err)
		return err
	}
	defer dbConn.Close()
	session := dbConn.NewSession(nil)
	datname, found := ctx.dbTokens["dbname"]
	if !found {
		ctx.dbg("dbDropDB", "empty database name")
		return fmt.Errorf("empty database name")
	}
	if dbExists, err := ctx.dbExists(session, datname); err == nil {
		if !dbExists {
			ctx.logger.Warn(fmt.Sprintf("database %s does not exist", datname))
			return nil
		}
	}
	if cb != nil && !cb(fmt.Sprintf("drop database: '%s'?", datname)) {
		ctx.dbg("dbDropDB", "callback and false return")
		ctx.logger.Warn("aborting...")
		return nil
	}
	ctx.logger.Inf(fmt.Sprintf("dropping database %s", datname))
	if _, err := session.Exec(fmt.Sprintf("drop database %s", datname)); err != nil {
		ctx.dbg("dbDropDB", err)
		return err
	}
	ctx.logger.Ok(fmt.Sprintf("database '%s' dropped", datname))
	return nil
}

func (ctx *PGMigrate) dbExecString(contents string, cb execCB) error {
	ctx.dbg("dbExecString")
	tx, err := ctx.dbGetTx()
	if err != nil {
		return err
	}
	if !ctx.config.AllInOneTx {
		ctx.dbg("dbExecString", "deferring rollback unless committed")
		defer tx.RollbackUnlessCommitted()
	}
	if _, err := tx.Exec(contents); err != nil {
		ctx.dbg("dbExecString", err)
		return err
	}
	if cb != nil {
		ctx.dbg("dbExecString", "executing callback function")
		if err := cb(tx); err != nil {
			ctx.dbg("dbExecString", err)
			return err
		}
	}
	if !ctx.config.AllInOneTx {
		ctx.dbg("dbExecString", "finishing db transaction")
		return ctx.dbFinish()
	}
	return nil
}

func (ctx *PGMigrate) dbMigrate(mig *migration, md migrateDirection) error {
	ctx.logger.Inf("migrating > %s (%s)", mig.Name, string(md))
	contents := mig.Up
	if md == migrateDown {
		contents = mig.Down
	}
	return ctx.dbExecString(contents, func(tx *dbr.Tx) error {
		if md == migrateUP {
			return ctx.dbInsertMigration(mig)
		}
		return ctx.dbDeleteMigration(mig)
	})
}

func (ctx *PGMigrate) dbInsertMigration(mig *migration) error {
	ctx.dbgJoin("dbInsertMigration", "inserting:", mig.Name)
	ctx.logger.Inf(fmt.Sprintf("inserting %s", mig.Name))
	_, err := ctx.tx.InsertInto(ctx.config.MigrationsTable).
		Columns("version", "name", "up", "down").
		Values(mig.Version, mig.Name, mig.Up, mig.Down).
		Exec()
	return err
}

func (ctx *PGMigrate) dbInsertMigrationBatch(migs []*migration) error {
	if ctx.tx == nil {
		if _, err := ctx.dbGetTx(); err != nil {
			return err
		}
	}
	for _, mig := range migs {
		if err := ctx.dbInsertMigration(mig); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *PGMigrate) dbDeleteMigration(mig *migration) error {
	ctx.dbgJoin("dbDeleteMigration", "deleting:", mig.Name)
	ctx.logger.Inf(fmt.Sprintf("deleting %s", mig.Name))
	_, err := ctx.tx.DeleteFrom(ctx.config.MigrationsTable).
		Where(dbr.Eq("version", mig.Version)).
		Exec()
	return err
}

func (ctx *PGMigrate) dbMigrationsTableExist() error {
	ctx.dbg("dbMigrationsTableExist")
	ctx.logger.Inf("checking for migrations table")
	return ctx.dbExecString(fmt.Sprintf(dbTableSchema, ctx.config.MigrationsTable), nil)
}

func (ctx *PGMigrate) dbGetMigrated() ([]*migration, error) {
	ctx.dbg("dbGetMigrated")
	tx, err := ctx.dbGetTx()
	if err != nil {
		return nil, err
	}
	migrations := []*migration{}
	if _, err := tx.Select("*").
		From(ctx.config.MigrationsTable).
		OrderDir("version", false).
		Load(&migrations); err != nil {
		ctx.dbg("dbGetMigrated", err)
		return nil, err
	}
	return migrations, nil
}

func (ctx *PGMigrate) dbFinish() error {
	ctx.dbg("dbFinish")
	if ctx.tx == nil && ctx.dbConn == nil {
		return nil
	}
	if ctx.config.DryRun {
		ctx.dbg("dbFinish", "dry run, not committing changes")
	}
	if ctx.tx != nil && !ctx.config.DryRun {
		ctx.dbg("dbFinish", "committing transaction")
		if err := ctx.tx.Commit(); err != nil {
			ctx.dbg("dbFinish", err)
			return err
		}
		ctx.dbg("dbFinish", "ok")
		ctx.tx = nil
	}

	if ctx.dbConn != nil {
		ctx.dbg("dbFinish", "closing db connection")
		if err := ctx.dbConn.Close(); err != nil {
			ctx.dbg("dbFinish", err)
			return err
		}
		ctx.dbConn = nil
		ctx.dbg("dbFinish", "ok")
	}
	return nil
}
