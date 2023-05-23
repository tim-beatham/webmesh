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

// Package node contains the webmesh node service.
package node

import (
	"context"
	"net"
	"net/netip"
	"strconv"

	"github.com/google/go-cmp/cmp"
	v1 "gitlab.com/webmesh/api/v1"
	"golang.org/x/exp/slog"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/webmesh/node/pkg/db/raftdb"
	"gitlab.com/webmesh/node/pkg/services/node/ipam"
	"gitlab.com/webmesh/node/pkg/services/node/peers"
	"gitlab.com/webmesh/node/pkg/util"
)

func (s *Server) Join(ctx context.Context, req *v1.JoinRequest) (*v1.JoinResponse, error) {
	if !s.store.IsLeader() {
		return nil, status.Errorf(codes.FailedPrecondition, "not leader")
	}

	if !s.ulaPrefix.IsValid() {
		ula, err := raftdb.New(s.store.WeakDB()).GetULAPrefix(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get ULA prefix: %v", err)
		}
		prefix, err := netip.ParsePrefix(ula)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse ULA prefix: %v", err)
		}
		s.ulaPrefix = prefix
	}

	// Validate inputs
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node id required")
	}
	publicKey, err := wgtypes.ParseKey(req.GetPublicKey())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid public key: %v", err)
	}
	var endpoint netip.AddrPort
	if req.GetEndpoint() != "" {
		endpoint, err = netip.ParseAddrPort(req.GetEndpoint())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid endpoint: %v", err)
		}
	}

	log := s.log.With("id", req.GetId())

	// Check if the peer already exists
	var peer *peers.Node
	peer, err = s.peers.Get(ctx, req.GetId())
	if err != nil && err != peers.ErrNodeNotFound {
		// Database error
		return nil, status.Errorf(codes.Internal, "failed to get peer: %v", err)
	} else if err == nil {
		log.Info("peer already exists, checking for updates")
		// Peer already exists, update it
		if peer.PublicKey.String() != publicKey.String() {
			peer.PublicKey = publicKey
		}
		if peer.GRPCPort != int(req.GetGrpcPort()) {
			peer.GRPCPort = int(req.GetGrpcPort())
		}
		if peer.RaftPort != int(req.GetRaftPort()) {
			peer.RaftPort = int(req.GetRaftPort())
		}
		if peer.Endpoint != endpoint {
			peer.Endpoint = endpoint
		}
		if !cmp.Equal(peer.AllowedIPs, req.GetAllowedIps()) {
			peer.AllowedIPs = req.GetAllowedIps()
		}
		if !cmp.Equal(peer.AvailableZones, req.GetAvailableZones()) {
			peer.AvailableZones = req.GetAvailableZones()
		}
		peer, err = s.peers.Update(ctx, peer)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update peer: %v", err)
		}
	} else {
		// New peer, create it
		log.Info("registering new peer")
		networkIPv6, err := util.Random64(s.ulaPrefix)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to generate IPv6 address: %v", err)
		}
		peer, err = s.peers.Create(ctx, &peers.CreateOptions{
			ID:             req.GetId(),
			PublicKey:      publicKey,
			Endpoint:       endpoint,
			NetworkIPv6:    networkIPv6,
			GRPCPort:       int(req.GetGrpcPort()),
			RaftPort:       int(req.GetRaftPort()),
			AllowedIPs:     req.GetAllowedIps(),
			AvailableZones: req.GetAvailableZones(),
			AssignASN:      req.GetAssignAsn(),
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create peer: %v", err)
		}
	}

	resp := &v1.JoinResponse{
		NetworkIpv6: peer.NetworkIPv6.String(),
		Asn:         peer.ASN,
	}

	// Check if we need to assign an ASN to an existing node
	if req.GetAssignAsn() && peer.ASN == 0 {
		log.Info("assigning ASN to peer")
		asn, err := s.peers.AssignASN(ctx, req.GetId())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to assign ASN: %v", err)
		}
		log.Info("assigned ASN to peer", slog.Int("asn", int(asn)))
		resp.Asn = asn
	}
	var lease ipam.Lease
	if req.GetAssignIpv4() {
		log.Info("assigning IPv4 address to peer")
		lease, err = s.ipam.Acquire(ctx, req.GetId())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to assign IPv4: %v", err)
		}
		log.Info("assigned IPv4 address to peer", slog.String("ipv4", lease.IPv4().String()))
		resp.AddressIpv4 = lease.IPv4().String()
	}
	// Add the peer as a non-voter to the store.
	var raftAddress string
	if req.GetAssignIpv4() && !req.GetPreferRaftIpv6() {
		// Prefer IPv4 for raft
		// TODO: doesn't work when we are IPv4 only. Need to fix this.
		// Basically if a single node is IPv4 only, we need to use IPv4 for raft.
		raftAddress = net.JoinHostPort(lease.IPv4().Addr().String(), strconv.Itoa(peer.RaftPort))
	} else {
		// Use IPv6
		raftAddress = net.JoinHostPort(peer.NetworkIPv6.Addr().String(), strconv.Itoa(peer.RaftPort))
	}
	log.Info("adding non-voter to cluster", slog.String("raft_address", raftAddress))
	if err := s.store.AddNonVoter(ctx, req.GetId(), raftAddress); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add non-voter: %v", err)
	}
	// List all wireguard peers
	peers, err := s.peers.ListPeers(ctx, req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list peers: %v", err)
	}
	resp.Peers = make([]*v1.WireguardPeer, len(peers))
	for i, p := range peers {
		peer := p
		resp.Peers[i] = &v1.WireguardPeer{
			PublicKey:   peer.PublicKey.String(),
			Asn:         peer.ASN,
			Endpoint:    peer.Endpoint.String(),
			AddressIpv4: peer.PrivateIPv4.String(),
			AddressIpv6: peer.NetworkIPv6.String(),
			AllowedIps:  peer.AllowedIPs,
		}
	}
	return resp, nil
}
