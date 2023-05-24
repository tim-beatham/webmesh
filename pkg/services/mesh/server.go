/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package mesh contains the webmesh Mesh service.
package mesh

import (
	v1 "gitlab.com/webmesh/api/v1"
	"golang.org/x/exp/slog"

	"gitlab.com/webmesh/node/pkg/meshdb/peers"
	"gitlab.com/webmesh/node/pkg/store"
)

// Server is the webmesh Mesh service.
type Server struct {
	v1.UnimplementedMeshServer

	store store.Store
	peers peers.Peers
	log   *slog.Logger
}

// NewServer returns a new Server.
func NewServer(store store.Store) *Server {
	return &Server{
		store: store,
		peers: peers.New(store),
		log:   slog.Default().With("component", "mesh-server"),
	}
}
