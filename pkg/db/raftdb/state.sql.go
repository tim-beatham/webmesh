// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.18.0
// source: state.sql

package raftdb

import (
	"context"
)

const getIPv4Prefix = `-- name: GetIPv4Prefix :one
SELECT value FROM mesh_state WHERE key = 'IPv4Prefix'
`

func (q *Queries) GetIPv4Prefix(ctx context.Context) (string, error) {
	row := q.db.QueryRowContext(ctx, getIPv4Prefix)
	var value string
	err := row.Scan(&value)
	return value, err
}

const getULAPrefix = `-- name: GetULAPrefix :one
SELECT value FROM mesh_state WHERE key = 'ULAPrefix'
`

func (q *Queries) GetULAPrefix(ctx context.Context) (string, error) {
	row := q.db.QueryRowContext(ctx, getULAPrefix)
	var value string
	err := row.Scan(&value)
	return value, err
}

const setIPv4Prefix = `-- name: SetIPv4Prefix :exec
INSERT into mesh_state (key, value) VALUES ('IPv4Prefix', ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value
`

func (q *Queries) SetIPv4Prefix(ctx context.Context, value string) error {
	_, err := q.db.ExecContext(ctx, setIPv4Prefix, value)
	return err
}

const setULAPrefix = `-- name: SetULAPrefix :exec
INSERT into mesh_state (key, value) VALUES ('ULAPrefix', ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value
`

func (q *Queries) SetULAPrefix(ctx context.Context, value string) error {
	_, err := q.db.ExecContext(ctx, setULAPrefix, value)
	return err
}
