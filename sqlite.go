package main

import (
	"database/sql"

	"github.com/hdpool/hdminer/liboo"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type Sqlite struct {
	sqldb  *sql.DB
	sqldbx *sqlx.DB
	wch    chan SubmitInfo
}

func (db *Sqlite) AddNonce(submit SubmitInfo) (err error) {
	var n int64
	sql := oo.NewSqler().Table("nonce_table").
		Where("Height", submit.Height).
		Where("Nonce", submit.Nonce).
		Count("*")
	err = db.sqldbx.Get(&n, sql)
	if err != nil {
		return
	}
	if n != 0 {
		return oo.NewError("%d:%s nonce exists", submit.Height, submit.Nonce)
	}
	db.wch <- submit

	return err
}

func (db *Sqlite) AddNonceBatch(arr []SubmitInfo) {
	var data []map[string]interface{}
	for _, sub := range arr {
		data = append(data, map[string]interface{}{
			"AccountId": sub.AccountId,
			"Coin":      sub.Coin,
			"Nonce":     sub.Nonce,
			"Deadline":  sub.Deadline,
			"Height":    sub.Height,
			"Ts":        sub.Ts,
		})
	}

	if len(data) > 0 {
		sql := oo.NewSqler().Table("nonce_table").
			OrIgnore().
			InsertBatch(data)
		if _, err := db.sqldb.Exec(sql); err != nil {
			oo.LogD("Failed to sql: %s, err=%v", sql, err)
		}
	}
}

func (db *Sqlite) GetLocalNonce(n int64) (sms []SubmitInfo, err error) {
	sql := oo.NewSqler().Table("nonce_table").Order("id desc").Limit(int(n)).Select("Nonce, COALESCE(Coin,'BHD') Coin, Deadline, AccountId, Height, Ts")
	err = db.sqldbx.Select(&sms, sql)
	if nil != err {
		oo.LogD("GetLocalNonce failed to sql[%s] err[%v]", sql, err)
		return
	}
	return
}

func (db *Sqlite) GetTopNonce(n int64) (sms []SubmitInfo, err error) {
	sql := oo.NewSqler().Table("nonce_table").Order("Deadline").Limit(int(n)).Select("Deadline, COALESCE(Coin,'BHD') Coin, Nonce, AccountId, Height, Ts")
	err = db.sqldbx.Select(&sms, sql)
	if nil != err {
		oo.LogD("GetTopNonce failed to sql[%s] err[%v]", sql, err)
		return
	}
	return
}

func (db *Sqlite) GetLessCount(deadline int64) (n int64, err error) {
	sql := oo.NewSqler().Table("nonce_table").Where("Deadline", "<", deadline).Count("*")
	err = db.sqldbx.Get(&n, sql)
	if err != nil {
		return 0, err
	}

	return
}

func (db *Sqlite) tryCreateTable() (err error) {

	sql := `
	create table if not exists "nonce_table" (
		"id" integer primary key autoincrement,
		"Nonce" varchar(128) NULL,
		"AccountId" varchar(128) NULL,
		"Deadline" integer,
		"Height" integer,
		"Ts" integer
		);
	create index nonce_ts_idx on nonce_table(Ts);
	create index nonce_deadline_idx on nonce_table(Deadline);

	create table if not exists "test_table" (
		"id" integer primary key autoincrement,
		"name" varchar(128) NULL
		);
	`
	if _, err = db.sqldb.Exec(sql); err != nil {
		oo.LogD("Failed to exec sql. err=%v", err)
	}

	return
}
func (db *Sqlite) trySupportCoins() (err error) {

	sql := `
	alter table nonce_table add Coin varchar(128) NULL; 
	create unique index index_height_nonce on nonce_table(Coin, Height, Nonce);
	`
	if _, err = db.sqldb.Exec(sql); err != nil {
		oo.LogD("Failed to exec sql. err=%v", err)
	}

	return
}

func (db *Sqlite) serializedWrite() {
	for {
		submit, ok := <-db.wch
		if !ok {
			break
		}
		sql := oo.NewSqler().Table("nonce_table").Insert(oo.Struct2Map(submit))
		if _, err := db.sqldb.Exec(sql); err != nil {
			oo.LogD("Failed to sql: %s, err=%v", sql, err)
		}
	}
}

func InitSqlite() (*Sqlite, error) {
	db := &Sqlite{}

	var err error
	db.sqldb, err = sql.Open("sqlite3", "cache.db")
	if err != nil {
		return nil, err
	}
	db.sqldbx = sqlx.NewDb(db.sqldb, "sqlite3")

	db.tryCreateTable()
	db.trySupportCoins()

	db.wch = make(chan SubmitInfo, 12)

	go db.serializedWrite()

	return db, nil
}
