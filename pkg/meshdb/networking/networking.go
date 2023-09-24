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

// Package networking contains interfaces to the database models for Network ACLs and Routes.
package networking

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/webmeshproj/webmesh/pkg/context"
	peergraph "github.com/webmeshproj/webmesh/pkg/meshdb/peers/graph"
	"github.com/webmeshproj/webmesh/pkg/storage"
)

const (
	// BootstrapNodesNetworkACLName is the name of the bootstrap nodes NetworkACL.
	BootstrapNodesNetworkACLName = "bootstrap-nodes"
	// NetworkACLsPrefix is where NetworkACLs are stored in the database.
	NetworkACLsPrefix = storage.RegistryPrefix + "network-acls"
	// RoutesPrefix is where Routes are stored in the database.
	RoutesPrefix = storage.RegistryPrefix + "routes"
	// GroupReference is the prefix of a node name that indicates it is a group reference.
	GroupReference = "group:"
)

// ErrACLNotFound is returned when a NetworkACL is not found.
var ErrACLNotFound = errors.New("network acl not found")

// ErrRouteNotFound is returned when a Route is not found.
var ErrRouteNotFound = errors.New("route not found")

// ErrInvalidACL is returned when a NetworkACL is invalid.
var ErrInvalidACL = errors.New("invalid network acl")

// GraphResolver is an interface that can return a mesh graph and resolve
// nodes by their ID.
type GraphResolver interface {
	// Graph returns the mesh graph.
	Graph() peergraph.Graph
	// Get returns a node by ID.
	Get(ctx context.Context, id string) (peergraph.MeshNode, error)
}

// Networking is the interface to the database models for network resources.
type Networking interface {
	// PutNetworkACL creates or updates a NetworkACL.
	PutNetworkACL(ctx context.Context, acl *v1.NetworkACL) error
	// GetNetworkACL returns a NetworkACL by name.
	GetNetworkACL(ctx context.Context, name string) (ACL, error)
	// DeleteNetworkACL deletes a NetworkACL by name.
	DeleteNetworkACL(ctx context.Context, name string) error
	// ListNetworkACLs returns a list of NetworkACLs.
	ListNetworkACLs(ctx context.Context) (ACLs, error)

	// PutRoute creates or updates a Route.
	PutRoute(ctx context.Context, route *v1.Route) error
	// GetRoute returns a Route by name.
	GetRoute(ctx context.Context, name string) (*v1.Route, error)
	// GetRoutesByNode returns a list of Routes for a given Node.
	GetRoutesByNode(ctx context.Context, nodeName string) ([]*v1.Route, error)
	// GetRoutesByCIDR returns a list of Routes for a given CIDR.
	GetRoutesByCIDR(ctx context.Context, cidr string) ([]*v1.Route, error)
	// DeleteRoute deletes a Route by name.
	DeleteRoute(ctx context.Context, name string) error
	// ListRoutes returns a list of Routes.
	ListRoutes(ctx context.Context) ([]*v1.Route, error)

	// FilterGraph filters the adjacency map in the given graph for the given node ID according
	// to the current network ACLs. If the ACL list is nil, an empty adjacency map is returned. An
	// error is returned on faiure building the initial map or any database error.
	FilterGraph(ctx context.Context, resolver GraphResolver, nodeID string) (peergraph.AdjacencyMap, error)
}

// New returns a new Networking interface.
func New(st storage.MeshStorage) Networking {
	return &networking{st}
}

type networking struct {
	storage.MeshStorage
}

// PutNetworkACL creates or updates a NetworkACL.
func (n *networking) PutNetworkACL(ctx context.Context, acl *v1.NetworkACL) error {
	err := ValidateACL(acl)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidACL, err)
	}
	key := fmt.Sprintf("%s/%s", NetworkACLsPrefix, acl.GetName())
	data, err := protojson.Marshal(acl)
	if err != nil {
		return fmt.Errorf("marshal network acl: %w", err)
	}
	err = n.PutValue(ctx, key, string(data), 0)
	if err != nil {
		return fmt.Errorf("put network acl: %w", err)
	}
	return nil
}

// GetNetworkACL returns a NetworkACL by name.
func (n *networking) GetNetworkACL(ctx context.Context, name string) (ACL, error) {
	key := fmt.Sprintf("%s/%s", NetworkACLsPrefix, name)
	data, err := n.GetValue(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return ACL{}, ErrACLNotFound
		}
		return ACL{}, fmt.Errorf("get network acl: %w", err)
	}
	acl := &v1.NetworkACL{}
	err = protojson.Unmarshal([]byte(data), acl)
	if err != nil {
		return ACL{}, fmt.Errorf("unmarshal network acl: %w", err)
	}
	return ACL{
		NetworkACL: acl,
		storage:    n.MeshStorage,
	}, nil
}

// DeleteNetworkACL deletes a NetworkACL by name.
func (n *networking) DeleteNetworkACL(ctx context.Context, name string) error {
	key := fmt.Sprintf("%s/%s", NetworkACLsPrefix, name)
	err := n.Delete(ctx, key)
	if err != nil && !errors.Is(err, storage.ErrKeyNotFound) {
		return fmt.Errorf("delete network acl: %w", err)
	}
	return nil
}

// ListNetworkACLs returns a list of NetworkACLs.
func (n *networking) ListNetworkACLs(ctx context.Context) (ACLs, error) {
	out := make(ACLs, 0)
	err := n.IterPrefix(ctx, NetworkACLsPrefix.String(), func(key, value string) error {
		if key == NetworkACLsPrefix.String() {
			return nil
		}
		acl := &v1.NetworkACL{}
		err := protojson.Unmarshal([]byte(value), acl)
		if err != nil {
			return fmt.Errorf("unmarshal network acl: %w", err)
		}
		out = append(out, &ACL{
			NetworkACL: acl,
			storage:    n.MeshStorage,
		})
		return nil
	})
	return out, err
}

// PutRoute creates or updates a Route.
func (n *networking) PutRoute(ctx context.Context, route *v1.Route) error {
	key := fmt.Sprintf("%s/%s", RoutesPrefix, route.GetName())
	data, err := protojson.Marshal(route)
	if err != nil {
		return fmt.Errorf("marshal route: %w", err)
	}
	err = n.PutValue(ctx, key, string(data), 0)
	if err != nil {
		return fmt.Errorf("put network route: %w", err)
	}
	return nil
}

// GetRoute returns a Route by name.
func (n *networking) GetRoute(ctx context.Context, name string) (*v1.Route, error) {
	key := fmt.Sprintf("%s/%s", RoutesPrefix, name)
	data, err := n.GetValue(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return nil, ErrRouteNotFound
		}
		return nil, fmt.Errorf("get network route: %w", err)
	}
	route := &v1.Route{}
	err = protojson.Unmarshal([]byte(data), route)
	if err != nil {
		return nil, fmt.Errorf("unmarshal network route: %w", err)
	}
	return route, nil
}

// GetRoutesByNode returns a list of Routes for a given Node.
func (n *networking) GetRoutesByNode(ctx context.Context, nodeName string) ([]*v1.Route, error) {
	routes, err := n.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list network routes: %w", err)
	}
	out := make([]*v1.Route, 0)
	for _, route := range routes {
		r := route
		if r.GetNode() == nodeName {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetRoutesByCIDR returns a list of Routes for a given CIDR.
func (n *networking) GetRoutesByCIDR(ctx context.Context, cidr string) ([]*v1.Route, error) {
	routes, err := n.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list network routes: %w", err)
	}
	out := make([]*v1.Route, 0)
	for _, route := range routes {
		r := route
		for _, destination := range r.GetDestinationCidrs() {
			if strings.HasPrefix(destination, cidr) {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

// DeleteRoute deletes a Route by name.
func (n *networking) DeleteRoute(ctx context.Context, name string) error {
	key := fmt.Sprintf("%s/%s", RoutesPrefix, name)
	err := n.Delete(ctx, key)
	if err != nil && !errors.Is(err, storage.ErrKeyNotFound) {
		return fmt.Errorf("delete network route: %w", err)
	}
	return nil
}

// ListRoutes returns a list of Routes.
func (n *networking) ListRoutes(ctx context.Context) ([]*v1.Route, error) {
	out := make([]*v1.Route, 0)
	err := n.IterPrefix(ctx, RoutesPrefix.String(), func(key, value string) error {
		if key == RoutesPrefix.String() {
			return nil
		}
		route := &v1.Route{}
		err := protojson.Unmarshal([]byte(value), route)
		if err != nil {
			return fmt.Errorf("unmarshal network route: %w", err)
		}
		out = append(out, route)
		return nil
	})
	return out, err
}

// FilterGraph filters the adjacency map in the given graph for the given node name according
// to the current network ACLs. If the ACL list is nil, an empty adjacency map is returned. An
// error is returned on faiure building the initial map or any database error. This implementation
// needs improvement to be more efficient and to allow edges so long as one of the routes encountered is
// allowed. Currently if a single route provided by a destination node is not allowed, the entire node
// is filtered out.
func (n *networking) FilterGraph(ctx context.Context, resolver GraphResolver, thisNodeID string) (peergraph.AdjacencyMap, error) {
	log := context.LoggerFrom(ctx)

	// Resolve the current node ID
	thisNode, err := resolver.Get(ctx, thisNodeID)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}

	// Gather all the ACLs and the current adjacency map
	acls, err := n.ListNetworkACLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list network acls: %w", err)
	}
	if len(acls) == 0 {
		return nil, nil
	}
	err = acls.Expand(ctx)
	if err != nil {
		return nil, fmt.Errorf("expand network acls: %w", err)
	}
	fullMap, err := peergraph.BuildAdjacencyMap(resolver.Graph())
	if err != nil {
		return nil, fmt.Errorf("build adjacency map: %w", err)
	}
	log.Debug("Full adjacency map", "from", thisNode.Id, "map", fullMap)

	// Start with a copy of the full map and filter out nodes that are not allowed to communicate
	// with the current node.
	filtered := make(peergraph.AdjacencyMap)
	filtered[thisNode.Id] = fullMap[thisNode.Id]

Nodes:
	for nodeID := range fullMap {
		if nodeID == thisNode.Id {
			continue
		}
		node, err := resolver.Get(ctx, nodeID)
		if err != nil {
			return nil, fmt.Errorf("get node: %w", err)
		}
		if !acls.AllowNodesToCommunicate(ctx, thisNode, node) {
			log.Debug("Nodes not allowed to communicate", "nodeA", thisNode, "nodeB", node)
			delete(filtered[thisNode.Id], node.GetId())
			continue Nodes
		}
		// If the destination node exposes additional routes, check if the nodes can communicate
		// via any of those routes.
		routes, err := n.GetRoutesByNode(ctx, node.GetId())
		if err != nil {
			return nil, fmt.Errorf("get routes by node: %w", err)
		}
		for _, route := range routes {
			for _, cidr := range route.GetDestinationCidrs() {
				prefix, err := netip.ParsePrefix(cidr)
				if err != nil {
					return nil, fmt.Errorf("parse prefix: %w", err)
				}
				var action Action
				if prefix.Addr().Is4() {
					action = Action{
						NetworkAction: &v1.NetworkAction{
							SrcNode: thisNode.Id,
							SrcCidr: thisNode.PrivateIpv4,
							DstNode: node.Id,
							DstCidr: cidr,
						},
					}
				} else {
					action = Action{
						NetworkAction: &v1.NetworkAction{
							SrcNode: thisNode.Id,
							SrcCidr: thisNode.PrivateIpv6,
							DstNode: node.Id,
							DstCidr: cidr,
						},
					}
				}
				if !acls.Accept(ctx, action) {
					log.Debug("filtering node", "node", node, "reason", "route not allowed", "action", action)
					delete(filtered[thisNode.Id], node.GetId())
					continue Nodes
				}
			}
		}
		filtered[node.GetId()] = make(map[string]peergraph.Edge)
	}
	for node := range filtered {
		edges, ok := fullMap[node]
		if !ok {
			continue
		}
	Peers:
		for peerID, edge := range edges {
			e := edge
			if peerID == thisNode.Id {
				filtered[node][peerID] = e
				continue
			}
			peer, err := resolver.Get(ctx, peerID)
			if err != nil {
				return nil, fmt.Errorf("get peer: %w", err)
			}
			if !acls.AllowNodesToCommunicate(ctx, thisNode, peer) {
				log.Debug("Nodes not allowed to communicate", "nodeA", thisNode, "nodeB", peer)
				continue Peers
			}
			// If the peer exposes additional routes, check if the nodes can communicate
			// via any of those routes.
			routes, err := n.GetRoutesByNode(ctx, peerID)
			if err != nil {
				return nil, fmt.Errorf("get routes by node: %w", err)
			}
			for _, route := range routes {
				for _, cidr := range route.GetDestinationCidrs() {
					prefix, err := netip.ParsePrefix(cidr)
					if err != nil {
						return nil, fmt.Errorf("parse prefix: %w", err)
					}
					var action v1.NetworkAction
					if prefix.Addr().Is4() {
						action = v1.NetworkAction{
							SrcNode: thisNode.Id,
							SrcCidr: thisNode.PrivateIpv4,
							DstNode: peerID,
							DstCidr: cidr,
						}
					} else {
						action = v1.NetworkAction{
							SrcNode: thisNode.Id,
							SrcCidr: thisNode.PrivateIpv6,
							DstNode: peerID,
							DstCidr: cidr,
						}
					}
					if !acls.Accept(ctx, Action{&action}) {
						log.Debug("filtering peer", "peer", peer, "reason", "route not allowed", "action", &action)
						continue Peers
					}
				}
			}
			filtered[node][peerID] = e
		}
	}

	log.Debug("Filtered adjacency map", "from", thisNode.Id, "map", filtered)
	return filtered, nil
}
