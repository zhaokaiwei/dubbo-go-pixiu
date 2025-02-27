/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package model

import (
	"fmt"
)

import (
	"github.com/dubbogo/dubbo-go-pixiu-filter/pkg/api"
)

type Metadata struct {
	Info map[string]MetadataValue
}

type MetadataValue interface{}

// Status is the components status

const (
	// Down
	Down api.Status = 0
	// Up
	Up      api.Status = 1
	Unknown api.Status = 2
)

const (
	ProtocolTypeHTTP ProtocolType = 0 + iota // support for 1.0
	ProtocolTypeTCP
	ProtocolTypeUDP
	ProtocolTypeHTTPS
	ProtocolTypeGRPC
)

const (
	REST_VALUE  = "REST"
	GRPC_VALUE  = "GRPC"
	DUBBO_VALUE = "DUBBO"
)

const (
	ApiTypeREST api.ApiType = 0 + iota // support for 1.0
	ApiTypeGRPC
	ApiTypeDUBBO
)

var (
	StatusName = map[int32]string{
		0: "Down",
		1: "Up",
		2: "Unknown",
	}

	StatusValue = map[string]int32{
		"Down":    0,
		"Up":      1,
		"Unknown": 2,
	}

	// ProtocolTypeName enum seq to protocol type name
	ProtocolTypeName = map[int32]string{
		0: "HTTP",
		1: "TCP",
		2: "UDP",
		3: "HTTPS",
		4: "GRPC",
	}

	// ProtocolTypeValue protocol type name to enum seq
	ProtocolTypeValue = map[string]int32{
		"HTTP":  0,
		"TCP":   1,
		"UDP":   2,
		"HTTPS": 3,
		"GRPC":  4,
	}

	ApiTypeName = map[int32]string{
		0: REST_VALUE,
		1: GRPC_VALUE,
		2: DUBBO_VALUE,
	}

	ApiTypeValue = map[string]int32{
		REST_VALUE:  0,
		GRPC_VALUE:  1,
		DUBBO_VALUE: 2,
	}
)

type (
	// ProtocolType protocol type enum
	ProtocolType int32

	// Address the address
	Address struct {
		SocketAddress SocketAddress `yaml:"socket_address" json:"socket_address" mapstructure:"socket_address"`
		Name          string        `yaml:"name" json:"name" mapstructure:"name"`
	}

	// SocketAddress specify either a logical or physical address and port, which are
	// used to tell server where to bind/listen, connect to upstream and find
	// management servers
	SocketAddress struct {
		ProtocolStr  string       `yaml:"protocol_type" json:"protocol_type" mapstructure:"protocol_type"`
		Protocol     ProtocolType `default:"http" yaml:"omitempty" json:"omitempty"`
		Address      string       `default:"0.0.0.0" yaml:"address" json:"address" mapstructure:"address"`
		Port         int          `default:"8881" yaml:"port" json:"port" mapstructure:"port"`
		ResolverName string       `yaml:"resolver_name" json:"resolver_name" mapstructure:"resolver_name"`
	}

	// ConfigSource
	ConfigSource struct {
		Path            string          `yaml:"path" json:"path" mapstructure:"path"`
		ApiConfigSource ApiConfigSource `yaml:"api_config_source" json:"api_config_source" mapstructure:"api_config_source"`
	}

	// ApiConfigSource
	ApiConfigSource struct {
		APIType     api.ApiType `yaml:"omitempty" json:"omitempty"`
		APITypeStr  string      `yaml:"api_type" json:"api_type" mapstructure:"api_type"`
		ClusterName []string    `yaml:"cluster_name" json:"cluster_name" mapstructure:"cluster_name"`
	}

	// HeaderValueOption
	HeaderValueOption struct {
		Header []HeaderValue `yaml:"header" json:"header" mapstructure:"header"`
		Append []bool        `yaml:"append" json:"append" mapstructure:"append"`
	}

	// HeaderValue
	HeaderValue struct {
		Key   string `yaml:"key" json:"key" mapstructure:"key"`
		Value string `yaml:"value" json:"value" mapstructure:"value"`
	}
)

func (a SocketAddress) GetAddress() string {
	return fmt.Sprintf("%s:%v", a.Address, a.Port)
}
