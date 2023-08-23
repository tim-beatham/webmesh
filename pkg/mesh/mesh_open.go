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

package mesh

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/multiformats/go-multiaddr"
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/protobuf/proto"

	"github.com/webmeshproj/webmesh/pkg/discovery/libp2p"
	"github.com/webmeshproj/webmesh/pkg/net"
	"github.com/webmeshproj/webmesh/pkg/plugins"
	"github.com/webmeshproj/webmesh/pkg/raft"
)

// Open opens the store.
func (s *meshStore) Open(ctx context.Context, features []v1.Feature) (err error) {
	if s.open.Load() {
		return ErrOpen
	}
	log := s.log
	// If bootstrap and force are set, clear the data directory.
	if s.opts.Bootstrap.Enabled && s.opts.Bootstrap.Force {
		log.Warn("force bootstrap enabled, clearing data directory")
		err = os.RemoveAll(s.opts.Raft.DataDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove all %q: %w", s.opts.Raft.DataDir, err)
		}
	}
	// Create the plugin manager
	s.plugins, err = plugins.NewManager(ctx, s.opts.Plugins)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}
	// Create the raft node
	s.opts.Raft.OnObservation = s.newObserver()
	s.opts.Raft.OnSnapshotRestore = func(ctx context.Context, meta *raft.SnapshotMeta, data io.ReadCloser) {
		// Dispatch the snapshot to any storage plugins.
		if err = s.plugins.ApplySnapshot(ctx, meta, data); err != nil {
			// This is non-fatal for now.
			s.log.Error("failed to apply snapshot to plugins", slog.String("error", err.Error()))
		}
	}
	s.opts.Raft.OnApplyLog = func(ctx context.Context, term, index uint64, log *v1.RaftLogEntry) {
		// Dispatch the log entry to any storage plugins.
		if _, err := s.plugins.ApplyRaftLog(ctx, &v1.StoreLogRequest{
			Term:  term,
			Index: index,
			Log:   log,
		}); err != nil {
			// This is non-fatal for now.
			s.log.Error("failed to apply log to plugins", slog.String("error", err.Error()))
		}
	}
	if s.opts.IsRaftMember() {
		s.raft = raft.New(s.opts.Raft, s)
	} else {
		s.raft = raft.NewPassthrough(s)
	}
	err = s.raft.Start(ctx, &raft.StartOptions{
		NodeID: s.ID(),
	})
	if err != nil {
		return fmt.Errorf("start raft: %w", err)
	}
	// Start serving storage queries for plugins.
	go s.plugins.ServeStorage(s.raft.Storage())
	handleErr := func(cause error) error {
		s.kvSubCancel()
		log.Error("failed to open store", slog.String("error", err.Error()))
		perr := s.plugins.Close()
		if perr != nil {
			log.Error("failed to close plugin manager", slog.String("error", perr.Error()))
		}
		cerr := s.raft.Stop(ctx)
		if cerr != nil {
			log.Error("failed to stop raft node", slog.String("error", cerr.Error()))
		}
		return cause
	}
	// Create the network manager
	s.nw = net.New(s.Storage(), &net.Options{
		NodeID:                s.ID(),
		InterfaceName:         s.opts.WireGuard.InterfaceName,
		ForceReplace:          s.opts.WireGuard.ForceInterfaceName,
		ListenPort:            s.opts.WireGuard.ListenPort,
		PersistentKeepAlive:   s.opts.WireGuard.PersistentKeepAlive,
		ForceTUN:              s.opts.WireGuard.ForceTUN,
		Modprobe:              s.opts.WireGuard.Modprobe,
		MTU:                   s.opts.WireGuard.MTU,
		RecordMetrics:         s.opts.WireGuard.RecordMetrics,
		RecordMetricsInterval: s.opts.WireGuard.RecordMetricsInterval,
		RaftPort:              s.raft.ListenPort(),
		GRPCPort:              s.opts.Mesh.GRPCAdvertisePort,
		ZoneAwarenessID:       s.opts.Mesh.ZoneAwarenessID,
		DialOptions:           s.grpcCreds(context.Background()),
		DisableIPv4:           s.opts.Mesh.NoIPv4,
		DisableIPv6:           s.opts.Mesh.NoIPv6,
	})
	// At this point we are open for business.
	s.open.Store(true)
	key, err := s.loadWireGuardKey(ctx)
	if err != nil {
		return fmt.Errorf("load wireguard key: %w", err)
	}
	if s.opts.Bootstrap.Enabled {
		// Attempt bootstrap.
		log.Info("bootstrapping cluster")
		if err = s.bootstrap(ctx, features, key); err != nil {
			return handleErr(fmt.Errorf("bootstrap: %w", err))
		}
	} else if s.opts.Mesh.JoinAddress != "" {
		// Attempt to join the cluster.
		err = s.join(ctx, features, s.opts.Mesh.JoinAddress, key)
		if err != nil {
			return handleErr(fmt.Errorf("join: %w", err))
		}
	} else if s.opts.Discovery != nil && s.opts.Discovery.UseKadDHT && s.opts.Discovery.PSK != "" {
		err = s.joinWithKadDHT(ctx, features, key)
		if err != nil {
			return handleErr(fmt.Errorf("join with kad dht: %w", err))
		}
	} else {
		// We neither had the bootstrap flag nor any join flags set.
		// This means we are possibly a single node cluster.
		// Recover our previous wireguard configuration and start up.
		if err := s.recoverWireguard(ctx); err != nil {
			return fmt.Errorf("recover wireguard: %w", err)
		}
	}
	// Register an update hook to watch for network changes.
	s.kvSubCancel, err = s.raft.Storage().Subscribe(context.Background(), "", s.onDBUpdate)
	if err != nil {
		return handleErr(fmt.Errorf("subscribe: %w", err))
	}
	if s.opts.Discovery != nil {
		if s.opts.Discovery.Announce {
			var peers []multiaddr.Multiaddr
			for _, p := range s.opts.Discovery.KadBootstrapServers {
				mul, err := multiaddr.NewMultiaddr(p)
				if err != nil {
					return handleErr(fmt.Errorf("new multiaddr: %w", err))
				}
				peers = append(peers, mul)
			}
			discover, err := libp2p.NewKadDHTAnnouncer(ctx, &libp2p.KadDHTOptions{
				PSK:            s.opts.Discovery.PSK,
				BootstrapPeers: peers,
				DiscoveryTTL:   time.Minute, // TODO: Make this configurable
			})
			if err != nil {
				return handleErr(fmt.Errorf("new kad dht announcer: %w", err))
			}
			if err := discover.Start(ctx); err != nil {
				return handleErr(fmt.Errorf("start peer discovery: %w", err))
			}
			go func() {
				conn, err := discover.Accept()
				if err != nil {
					log.Error("failed to accept peer connection from discovery service", slog.String("error", err.Error()))
					return
				}
				go s.handleIncomingDiscoveryPeer(conn)
			}()
			s.discovery = discover
		}
	}
	return nil
}

func (s *meshStore) handleIncomingDiscoveryPeer(conn io.ReadWriteCloser) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15) // TODO: Make this configurable
	defer cancel()
	// Read a join request off the wire
	var req v1.JoinRequest
	b := make([]byte, 4096)
	n, err := conn.Read(b)
	if err != nil {
		s.log.Error("failed to read join request from discovered peer", slog.String("error", err.Error()))
		return
	}
	if err := proto.Unmarshal(b[:n], &req); err != nil {
		s.log.Error("failed to unmarshal join request from discovered peer", slog.String("error", err.Error()))
		return
	}
	// Forward the request to the leader
	c, err := s.DialLeader(ctx)
	if err != nil {
		s.log.Error("failed to dial leader", slog.String("error", err.Error()))
		return
	}
	defer c.Close()
	resp, err := v1.NewMembershipClient(c).Join(ctx, &req)
	if err != nil {
		s.log.Error("failed to join cluster", slog.String("error", err.Error()))
		// Attempt to write the raw error back to the peer
		b = []byte("ERROR: " + err.Error())
		if _, err := conn.Write(b); err != nil {
			s.log.Error("failed to write error to discovered peer", slog.String("error", err.Error()))
		}
		return
	}
	// Write the response back to the peer
	b, err = proto.Marshal(resp)
	if err != nil {
		s.log.Error("failed to marshal join response", slog.String("error", err.Error()))
		return
	}
	if _, err := conn.Write(b); err != nil {
		s.log.Error("failed to write join response to discovered peer", slog.String("error", err.Error()))
		return
	}
}
