// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.18.0

package raftdb

import (
	"database/sql"
	"time"
)

type Asn struct {
	Asn       int64     `json:"asn"`
	NodeID    string    `json:"node_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Lease struct {
	NodeID    string    `json:"node_id"`
	Ipv4      string    `json:"ipv4"`
	CreatedAt time.Time `json:"created_at"`
}

type MeshState struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Node struct {
	ID             string         `json:"id"`
	PublicKey      sql.NullString `json:"public_key"`
	RaftPort       int64          `json:"raft_port"`
	GrpcPort       int64          `json:"grpc_port"`
	Endpoint       sql.NullString `json:"endpoint"`
	NetworkIpv6    sql.NullString `json:"network_ipv6"`
	AllowedIps     sql.NullString `json:"allowed_ips"`
	AvailableZones sql.NullString `json:"available_zones"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type NodePrivateRpcAddress struct {
	NodeID  string       `json:"node_id"`
	Address sql.NullBool `json:"address"`
}

type NodePublicRpcAddress struct {
	NodeID  string       `json:"node_id"`
	Address sql.NullBool `json:"address"`
}
