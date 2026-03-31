package models

import "github.com/jackc/pgx/v5"

func isNotFound(err error) bool {
	return err == pgx.ErrNoRows
}
