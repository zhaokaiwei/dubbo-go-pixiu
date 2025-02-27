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

package grpcproxy

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	stdHttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

import (
	"github.com/golang/protobuf/jsonpb" //nolint
	"github.com/golang/protobuf/proto"  //nolint
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	perrors "github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

import (
	"github.com/apache/dubbo-go-pixiu/pkg/common/constant"
	"github.com/apache/dubbo-go-pixiu/pkg/common/extension/filter"
	"github.com/apache/dubbo-go-pixiu/pkg/context/http"
	"github.com/apache/dubbo-go-pixiu/pkg/logger"
	"github.com/apache/dubbo-go-pixiu/pkg/server"
)

const (
	// Kind is the kind of Fallback.
	Kind = constant.HTTPGrpcProxyFilter

	loggerHeader = "[grpc-proxy]"
)

var (
	fsrc fileSource
)

func init() {
	filter.RegisterHttpFilter(&Plugin{})
}

type (
	// Plugin is grpc filter plugin.
	Plugin struct {
	}

	// Filter is grpc filter instance
	Filter struct {
		cfg *Config
		// hold grpc.ClientConns, key format: cluster name + "." + endpoint
		pools map[string]*sync.Pool

		extReg     dynamic.ExtensionRegistry
		registered map[string]bool
	}

	// Config describe the config of AccessFilter
	Config struct {
		Path  string  `yaml:"path" json:"path"`
		Rules []*Rule `yaml:"rules" json:"rules"` //nolint
	}

	Rule struct {
		Selector string `yaml:"selector" json:"selector"`
		Match    Match  `yaml:"match" json:"match"`
	}

	Match struct {
		Method string `yaml:"method" json:"method"` //nolint
	}
)

func (p *Plugin) Kind() string {
	return Kind
}

func (p *Plugin) CreateFilter() (filter.HttpFilter, error) {
	return &Filter{cfg: &Config{}}, nil
}

func (f *Filter) PrepareFilterChain(ctx *http.HttpContext) error {
	ctx.AppendFilterFunc(f.Handle)
	return nil
}

// getServiceAndMethod first return value is package.service, second one is method name
func getServiceAndMethod(path string) (string, string) {
	pos := strings.LastIndex(path, "/")
	if pos < 0 {
		return "", ""
	}

	mth := path[pos+1:]
	prefix := strings.TrimSuffix(path, "/"+mth)

	pos = strings.LastIndex(prefix, "/")
	if pos < 0 {
		return "", ""
	}

	svc := prefix[pos+1:]
	return svc, mth
}

// Handle use the default http to grpc transcoding strategy https://cloud.google.com/endpoints/docs/grpc/transcoding
func (f *Filter) Handle(c *http.HttpContext) {
	svc, mth := getServiceAndMethod(c.GetUrl())

	dscp, err := fsrc.FindSymbol(svc)
	if err != nil {
		logger.Errorf("%s err {%s}", loggerHeader, "request path invalid")
		c.Err = perrors.New("method not allow")
		c.Next()
		return
	}

	svcDesc, ok := dscp.(*desc.ServiceDescriptor)
	if !ok {
		logger.Errorf("%s err {service not expose, %s}", loggerHeader, svc)
		c.Err = perrors.New(fmt.Sprintf("service not expose, %s", svc))
		c.Next()
		return
	}

	mthDesc := svcDesc.FindMethodByName(mth)

	err = f.registerExtension(mthDesc)
	if err != nil {
		logger.Errorf("%s err {%s}", loggerHeader, "register extension failed")
		c.Err = err
		c.Next()
		return
	}

	msgFac := dynamic.NewMessageFactoryWithExtensionRegistry(&f.extReg)
	grpcReq := msgFac.NewMessage(mthDesc.GetInputType())

	err = jsonToProtoMsg(c.Request.Body, grpcReq)
	if err != nil && !errors.Is(err, io.EOF) {
		logger.Errorf("%s err {failed to convert json to proto msg, %s}", loggerHeader, err.Error())
		c.Err = err
		c.Next()
		return
	}

	var clientConn *grpc.ClientConn
	re := c.GetRouteEntry()
	logger.Debugf("%s client choose endpoint from cluster :%v", loggerHeader, re.Cluster)

	e := server.GetClusterManager().PickEndpoint(re.Cluster)
	if e == nil {
		logger.Errorf("%s err {cluster not exists}", loggerHeader)
		c.Err = perrors.New("cluster not exists")
		c.Next()
		return
	}

	ep := e.Address.GetAddress()

	p, ok := f.pools[strings.Join([]string{re.Cluster, ep}, ".")]
	if !ok {
		p = &sync.Pool{}
	}

	clientConn, ok = p.Get().(*grpc.ClientConn)
	if !ok || clientConn == nil {
		// TODO(Kenway): Support Credential and TLS
		clientConn, err = grpc.DialContext(c.Ctx, ep, grpc.WithInsecure())
		if err != nil || clientConn == nil {
			logger.Errorf("%s err {failed to connect to grpc service provider}", loggerHeader)
			c.Err = err
			c.Next()
			return
		}
	}

	stub := grpcdynamic.NewStubWithMessageFactory(clientConn, msgFac)

	// metadata in grpc has the same feature in http
	md := mapHeaderToMetadata(c.AllHeaders())
	ctx := metadata.NewOutgoingContext(c.Ctx, md)

	md = metadata.MD{}
	t := metadata.MD{}

	resp, err := Invoke(ctx, stub, mthDesc, grpcReq, grpc.Header(&md), grpc.Trailer(&t))
	// judge err is server side error or not
	if st, ok := status.FromError(err); !ok || isServerError(st) {
		logger.Error("%s err {failed to invoke grpc service provider, %s}", loggerHeader, err.Error())
		c.Err = err
		c.Next()
		return
	}

	res, err := protoMsgToJson(resp)
	if err != nil {
		logger.Error("%s err {failed to convert proto msg to json, %s}", loggerHeader, err.Error())
		c.Err = err
		c.Next()
		return
	}

	h := mapMetadataToHeader(md)
	th := mapMetadataToHeader(t)

	// let response filter handle resp
	c.SourceResp = &stdHttp.Response{
		StatusCode: stdHttp.StatusOK,
		Header:     h,
		Body:       ioutil.NopCloser(strings.NewReader(res)),
		Trailer:    th,
		Request:    c.Request,
	}
	p.Put(clientConn)
	c.Next()
}

func (f *Filter) registerExtension(mthDesc *desc.MethodDescriptor) error {
	err := RegisterExtension(&f.extReg, mthDesc.GetInputType(), f.registered)
	if err != nil {
		return perrors.New("register extension failed")
	}

	err = RegisterExtension(&f.extReg, mthDesc.GetOutputType(), f.registered)
	if err != nil {
		return perrors.New("register extension failed")
	}
	return nil
}

func RegisterExtension(extReg *dynamic.ExtensionRegistry, msgDesc *desc.MessageDescriptor, registered map[string]bool) error {
	msgType := msgDesc.GetFullyQualifiedName()
	if _, ok := registered[msgType]; ok {
		return nil
	}

	if len(msgDesc.GetExtensionRanges()) > 0 {
		fds, err := fsrc.AllExtensionsForType(msgType)
		if err != nil {
			return fmt.Errorf("failed to find msg type {%s} in file source", msgType)
		}

		err = extReg.AddExtension(fds...)
		if err != nil {
			return fmt.Errorf("failed to register extensions of msgType {%s}, err is {%s}", msgType, err.Error())
		}
	}

	for _, fd := range msgDesc.GetFields() {
		if fd.GetMessageType() != nil {
			err := RegisterExtension(extReg, fd.GetMessageType(), registered)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func mapHeaderToMetadata(header stdHttp.Header) metadata.MD {
	md := metadata.MD{}
	for key, val := range header {
		md.Append(key, val...)
	}
	return md
}

func mapMetadataToHeader(md metadata.MD) stdHttp.Header {
	h := stdHttp.Header{}
	for key, val := range md {
		for _, v := range val {
			h.Add(key, v)
		}
	}
	return h
}

func jsonToProtoMsg(reader io.Reader, msg proto.Message) error {
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}
	return jsonpb.UnmarshalString(string(body), msg)
}

func protoMsgToJson(msg proto.Message) (string, error) {
	m := jsonpb.Marshaler{}
	return m.MarshalToString(msg)
}

func isServerError(st *status.Status) bool {
	return st.Code() == codes.DeadlineExceeded || st.Code() == codes.ResourceExhausted || st.Code() == codes.Internal ||
		st.Code() == codes.Unavailable
}

func (f *Filter) Config() interface{} {
	return f.cfg
}

func (f *Filter) Apply() error {
	gc := f.cfg

	cur := gc.Path
	if !filepath.IsAbs(cur) {
		ex, err := os.Executable()
		if err != nil {
			return err
		}
		cur = filepath.Dir(ex) + string(os.PathSeparator) + gc.Path
	}

	logger.Infof("%s load proto files from %s", loggerHeader, cur)
	fileLists := make([]string, 0)
	items, err := ioutil.ReadDir(cur)
	if err != nil {
		return err
	}

	for _, item := range items {
		if !item.IsDir() {
			sp := strings.Split(item.Name(), ".")
			length := len(sp)
			if length >= 2 && sp[length-1] == "proto" {
				fileLists = append(fileLists, item.Name())
			}
		}
	}

	if err != nil {
		return err
	}
	err = f.initFromFileDescriptor([]string{gc.Path}, fileLists...)
	if err != nil {
		return err
	}
	return nil
}
