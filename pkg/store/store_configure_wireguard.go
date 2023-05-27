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

package store

import (
	"context"
	"fmt"
	"net/netip"

	"golang.org/x/exp/slog"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"gitlab.com/webmesh/node/pkg/firewall"
	"gitlab.com/webmesh/node/pkg/meshdb/meshgraph"
	"gitlab.com/webmesh/node/pkg/wireguard"
)

func (s *store) ConfigureWireguard(ctx context.Context, key wgtypes.Key, networkv4, networkv6 netip.Prefix) error {
	s.wgmux.Lock()
	defer s.wgmux.Unlock()
	s.wgopts.NetworkV4 = networkv4
	s.wgopts.NetworkV6 = networkv6
	s.wgopts.IsPublic = s.opts.NodeEndpoint != ""
	s.log.Info("configuring wireguard interface", slog.Any("options", s.wgopts))
	var err error
	if s.fw == nil {
		s.fw, err = firewall.New(&firewall.Options{
			DefaultPolicy: firewall.PolicyAccept,
			WireguardPort: uint16(s.wgopts.ListenPort),
		})
		if err != nil {
			return fmt.Errorf("new firewall: %w", err)
		}
	}
	if s.wg == nil {
		s.wg, err = wireguard.New(ctx, s.wgopts)
		if err != nil {
			return fmt.Errorf("new wireguard: %w", err)
		}
		err = s.wg.Up(ctx)
		if err != nil {
			return fmt.Errorf("wireguard up: %w", err)
		}
	}
	err = s.wg.Configure(ctx, key, s.wgopts.ListenPort)
	if err != nil {
		return fmt.Errorf("wireguard configure: %w", err)
	}
	if networkv4.IsValid() {
		err = s.wg.AddRoute(ctx, networkv4)
		if err != nil && !wireguard.IsRouteExists(err) {
			return fmt.Errorf("wireguard add ipv4 route: %w", err)
		}
	}
	if networkv6.IsValid() {
		err = s.wg.AddRoute(ctx, networkv6)
		if err != nil && !wireguard.IsRouteExists(err) {
			return fmt.Errorf("wireguard add ipv6 route: %w", err)
		}
	}
	err = s.fw.AddWireguardForwarding(ctx, s.wg.Name())
	if err != nil {
		return fmt.Errorf("failed to add wireguard forwarding rule: %w", err)
	}
	if s.wgopts.Masquerade {
		err = s.fw.AddMasquerade(ctx, s.wg.Name())
		if err != nil {
			return fmt.Errorf("failed to add masquerade rule: %w", err)
		}
	}
	return nil
}

func (s *store) RefreshWireguardPeers(ctx context.Context) error {
	if s.wg == nil {
		return nil
	}
	s.wgmux.Lock()
	defer s.wgmux.Unlock()
	dag, err := meshgraph.New(s).Build(ctx)
	if err != nil {
		s.log.Error("build dag", slog.String("error", err.Error()))
		return err
	}
	err = s.walkMeshDescendants(dag)
	if err != nil {
		s.log.Error("walk mesh descendants", slog.String("error", err.Error()))
		return nil
	}
	return nil
}

func (s *store) walkMeshDescendants(graph meshgraph.Graph) error {
	adjacencyMap, err := graph.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("adjacency map: %w", err)
	}
	slog.Debug("current adjacency map", slog.Any("map", adjacencyMap))
	ourDescendants := adjacencyMap[string(s.nodeID)]
	if len(ourDescendants) == 0 {
		s.log.Debug("no descendants found in mesh DAG")
		return nil
	}
	for descendant, edge := range ourDescendants {
		desc, _ := graph.Vertex(descendant)
		// Each direct child is a wireguard peer
		peer := wireguard.Peer{
			ID:         desc.ID,
			PublicKey:  desc.PublicKey,
			Endpoint:   desc.PublicEndpoint,
			AllowedIPs: make([]netip.Prefix, 0),
		}
		if desc.PrivateIPv4.IsValid() {
			peer.AllowedIPs = append(peer.AllowedIPs, desc.PrivateIPv4)
		}
		if desc.PrivateIPv6.IsValid() {
			peer.AllowedIPs = append(peer.AllowedIPs, desc.PrivateIPv6)
		}
		descTargets := adjacencyMap[edge.Target]
		if len(descTargets) > 0 {
			for descTarget := range descTargets {
				if _, ok := ourDescendants[descTarget]; !ok && descTarget != string(s.nodeID) {
					target, _ := graph.Vertex(descTarget)
					if target.PrivateIPv4.IsValid() {
						peer.AllowedIPs = append(peer.AllowedIPs, target.PrivateIPv4)
					}
					if target.PrivateIPv6.IsValid() {
						peer.AllowedIPs = append(peer.AllowedIPs, target.PrivateIPv6)
					}
				}
			}
		}
		slog.Debug("allowed ips for descendant",
			slog.Any("allowed_ips", peer.AllowedIPs), slog.String("descendant", desc.ID))
		if err := s.wg.PutPeer(context.Background(), &peer); err != nil {
			return fmt.Errorf("put peer: %w", err)
		}
	}
	return nil
}
