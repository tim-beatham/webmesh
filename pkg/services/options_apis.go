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

package services

import (
	"errors"
	"flag"
	"strings"

	"github.com/webmeshproj/webmesh/pkg/util/envutil"
)

const (
	LeaderProxyDisabledEnvVar = "SERVICES_API_DISABLE_LEADER_PROXY"
	MeshEnabledEnvVar         = "SERVICES_API_MESH"
	AdminEnabledEnvVar        = "SERVICES_API_ADMIN"
	WebRTCEnabledEnvVar       = "SERVICES_API_WEBRTC"
	WebRTCSTUNServersEnvVar   = "SERVICES_API_STUN_SERVERS"
)

// APIOptions are the options for which APIs to register and expose.
type APIOptions struct {
	// DisableLeaderProxy is true if the leader proxy should be disabled.
	DisableLeaderProxy bool `json:"disable-leader-proxy,omitempty" yaml:"disable-leader-proxy,omitempty" toml:"disable-leader-proxy,omitempty" mapstructure:"disable-leader-proxy,omitempty"`
	// Mesh is true if the mesh API should be registered.
	Mesh bool `json:"mesh,omitempty" yaml:"mesh,omitempty" toml:"mesh,omitempty" mapstructure:"mesh,omitempty"`
	// Admin is true if the admin API should be registered.
	Admin bool `json:"admin,omitempty" yaml:"admin,omitempty" toml:"admin,omitempty" mapstructure:"admin,omitempty"`
	// WebRTC is true if the WebRTC API should be registered.
	WebRTC bool `json:"webrtc,omitempty" yaml:"webrtc,omitempty" toml:"webrtc,omitempty" mapstructure:"webrtc,omitempty"`
	// STUNServers is a comma separated list of STUN servers to use if the WebRTC API is enabled.
	STUNServers string `json:"stun-servers,omitempty" yaml:"stun-servers,omitempty" toml:"stun-servers,omitempty" mapstructure:"stun-servers,omitempty"`
}

// NewAPIOptions creates a new APIOptions with default values.
func NewAPIOptions() *APIOptions {
	return &APIOptions{
		STUNServers: "stun:stun.l.google.com:19302",
	}
}

// BindFlags binds the flags. The options are returned
func (o *APIOptions) BindFlags(fs *flag.FlagSet, prefix ...string) {
	var p string
	if len(prefix) > 0 {
		p = strings.Join(prefix, ".") + "."
	}
	fs.BoolVar(&o.DisableLeaderProxy, p+"services.api.disable-leader-proxy", envutil.GetEnvDefault(LeaderProxyDisabledEnvVar, "false") == "true",
		"Disable the leader proxy.")
	fs.BoolVar(&o.Admin, p+"services.api.admin", envutil.GetEnvDefault(AdminEnabledEnvVar, "false") == "true",
		"Enable the admin API.")
	fs.BoolVar(&o.Mesh, p+"services.api.mesh", envutil.GetEnvDefault(MeshEnabledEnvVar, "false") == "true",
		"Enable the mesh API.")
	fs.BoolVar(&o.WebRTC, p+"services.api.webrtc", envutil.GetEnvDefault(WebRTCEnabledEnvVar, "false") == "true",
		"Enable the WebRTC API.")
	fs.StringVar(&o.STUNServers, p+"services.api.stun-servers", envutil.GetEnvDefault(WebRTCSTUNServersEnvVar, "stun:stun.l.google.com:19302"),
		"STUN servers to use.")
}

// Validate validates the options.
func (o *APIOptions) Validate() error {
	if o == nil {
		return nil
	}
	if o.WebRTC && o.STUNServers == "" {
		return errors.New("STUN servers must be specified if the WebRTC API is enabled")
	}
	return nil
}

// DeepCopy returns a deep copy of the options.
func (o *APIOptions) DeepCopy() *APIOptions {
	if o == nil {
		return nil
	}
	no := &APIOptions{}
	*no = *o
	return no
}
