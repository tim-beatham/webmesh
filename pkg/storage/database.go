/*
Copyright 2023 Avi Zimmerman <avi.zimmerman@gmail.com>

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

package storage

// Database is the interface for the mesh database. It provides access to all
// storage interfaces.
type Database interface {
	// Peers returns the interface for managing nodes in the mesh.
	Peers() Peers
	// RBAC returns the interface for managing RBAC policies in the mesh.
	RBAC() RBAC
	// MeshState returns the interface for querying mesh state.
	MeshState() MeshState
	// TODO: Networking
}
